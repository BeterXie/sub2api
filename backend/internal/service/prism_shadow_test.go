//go:build unit

package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type prismShadowRepoStub struct {
	*sparkShadowRepoStub
	patchError error
}

func (r *prismShadowRepoStub) UpdateExtra(ctx context.Context, id int64, updates map[string]any) error {
	if r.patchError != nil {
		return r.patchError
	}
	account, err := r.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if account.Extra == nil {
		account.Extra = make(map[string]any)
	}
	for key, value := range updates {
		account.Extra[key] = value
	}
	return nil
}

func newPrismShadowParent(t *testing.T) (*prismShadowRepoStub, *adminServiceImpl, *Account) {
	t.Helper()
	repo := &prismShadowRepoStub{sparkShadowRepoStub: newSparkShadowRepoStub()}
	parent := &Account{Name: "parent", Platform: PlatformOpenAI, Type: AccountTypeOAuth,
		Status: StatusActive, Schedulable: true, Concurrency: 3, Priority: 50,
		Credentials: map[string]any{"access_token": "parent-access", "refresh_token": "parent-refresh"},
		Extra:       map[string]any{"openai_prism_fallback": true, "codex_5h_used_percent": 37.0}}
	require.NoError(t, repo.Create(context.Background(), parent))
	storedParent, err := repo.GetByID(context.Background(), parent.ID)
	require.NoError(t, err)
	return repo, &adminServiceImpl{accountRepo: repo}, storedParent
}

func TestPrismShadowCreationAndLifecycle(t *testing.T) {
	ctx := context.Background()
	repo, admin, parent := newPrismShadowParent(t)
	parent.GroupIDs = []int64{7}
	proxyID := int64(3)
	parent.ProxyID = &proxyID
	spark, err := admin.CreateShadow(ctx, parent.ID, ShadowOptions{})
	require.NoError(t, err)
	shadow, err := admin.CreateShadow(ctx, parent.ID, ShadowOptions{QuotaDimension: QuotaDimensionPrism})
	require.NoError(t, err)
	require.Equal(t, "parent (Prism)", shadow.Name)
	require.Equal(t, parent.ID, *shadow.ParentAccountID)
	require.Equal(t, QuotaDimensionPrism, shadow.QuotaDimension)
	require.Equal(t, []int64{7}, shadow.GroupIDs)
	require.Equal(t, parent.ProxyID, shadow.ProxyID)
	require.Equal(t, parent.Priority, shadow.Priority)
	require.Equal(t, 2, shadow.Concurrency)
	require.Len(t, shadow.Credentials, 1)
	require.NotContains(t, shadow.Credentials, "access_token")
	require.NotContains(t, shadow.Credentials, "refresh_token")
	storedParent, err := repo.GetByID(ctx, parent.ID)
	require.NoError(t, err)
	require.False(t, accountHasPrismBrowser(storedParent))
	require.False(t, accountHasPrismFallback(storedParent))
	require.Equal(t, 37.0, storedParent.Extra["codex_5h_used_percent"])
	require.Equal(t, "parent-refresh", storedParent.Credentials["refresh_token"])
	_, err = admin.CreateShadow(ctx, parent.ID, ShadowOptions{QuotaDimension: QuotaDimensionPrism})
	require.Error(t, err)
	_, err = admin.CreateShadow(ctx, parent.ID, ShadowOptions{})
	require.Error(t, err)
	_, err = admin.CreateShadow(ctx, shadow.ID, ShadowOptions{QuotaDimension: QuotaDimensionPrism})
	require.Error(t, err)
	require.NoError(t, admin.DeleteAccount(ctx, parent.ID))
	require.NotContains(t, repo.accounts, spark.ID)
	require.NotContains(t, repo.accounts, shadow.ID)
}

func TestPrismShadowConcurrencyUsesProjectPoolCapacity(t *testing.T) {
	for _, tc := range []struct {
		requested int
		want      int
	}{{0, 2}, {1, 1}, {2, 2}, {9, 2}} {
		t.Run("requested_"+strconv.Itoa(tc.requested), func(t *testing.T) {
			_, admin, parent := newPrismShadowParent(t)
			shadow, err := admin.CreateShadow(context.Background(), parent.ID, ShadowOptions{
				QuotaDimension: QuotaDimensionPrism, Concurrency: tc.requested,
			})
			require.NoError(t, err)
			require.Equal(t, tc.want, shadow.Concurrency)
			require.Equal(t, 3, parent.Concurrency)
		})
	}
}

func TestPrismShadowParentRouteFailureRemovesNewShadow(t *testing.T) {
	repo, admin, parent := newPrismShadowParent(t)
	repo.patchError = errors.New("parent update failed")
	_, err := admin.CreateShadow(context.Background(), parent.ID, ShadowOptions{QuotaDimension: QuotaDimensionPrism})
	require.Error(t, err)
	require.Len(t, repo.accounts, 1)
	storedParent, err := repo.GetByID(context.Background(), parent.ID)
	require.NoError(t, err)
	require.True(t, accountHasPrismFallback(storedParent))
}

func TestPrismShadowUsesLiveParentCredentialsAndProfile(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo, admin, parent := newPrismShadowParent(t)
	shadow, err := admin.CreateShadow(context.Background(), parent.ID, ShadowOptions{QuotaDimension: QuotaDimensionPrism})
	require.NoError(t, err)
	parent, err = repo.GetByID(context.Background(), parent.ID)
	require.NoError(t, err)
	parent.Proxy = &Proxy{Protocol: "http", Host: "proxy.example.test", Port: 3128}
	shadow.Proxy = &Proxy{Protocol: "http", Host: "stale-proxy.invalid", Port: 9999}
	bridgeCalls := 0
	bridge := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bridgeCalls++
		require.Equal(t, strconv.FormatInt(parent.ID, 10), r.Header.Get("X-Prism-Account-ID"))
		require.Equal(t, parent.GetOpenAIAccessToken(), r.Header.Get("X-Prism-OAuth-Token"))
		require.Equal(t, parent.Proxy.URL(), r.Header.Get("X-Prism-Proxy"))
		_, _ = io.WriteString(w, prismFallbackFixtureResponse)
	}))
	defer bridge.Close()
	s, _ := prismTestService(bridge.URL)
	s.accountRepo = repo
	upstream := &httpUpstreamRecorder{}
	s.httpUpstream = upstream
	for _, token := range []string{"parent-access", "rotated-parent-access"} {
		parent.Credentials["access_token"] = token
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
		result, err := s.Forward(context.Background(), c, shadow, []byte(prismFallbackFixtureBody))
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, rec.Code)
		require.True(t, result.UsageUnavailable)
		require.Equal(t, "gpt-6.1-sol", result.UpstreamModel)
	}
	require.Equal(t, 2, bridgeCalls)
	require.Empty(t, upstream.requests)
	require.NotContains(t, shadow.Credentials, "access_token")
}

func TestPrismShadowQuotaAndAdmissionAreIndependent(t *testing.T) {
	repo, admin, parent := newPrismShadowParent(t)
	shadow, err := admin.CreateShadow(context.Background(), parent.ID, ShadowOptions{QuotaDimension: QuotaDimensionPrism})
	require.NoError(t, err)
	parent, err = repo.GetByID(context.Background(), parent.ID)
	require.NoError(t, err)
	until := time.Now().Add(time.Hour)
	parent.RateLimitResetAt = &until
	lookup := func(int64) *Account { return parent }
	require.True(t, parentHealthyForShadow(shadow, lookup), "OpenAI quota must not block Prism")
	require.True(t, shadow.IsSchedulable())
	require.Equal(t, shadow.ID, shadow.RPMAccountID())
	require.True(t, shadow.IsModelSupported("gpt-6-astra"))
	require.False(t, shadow.IsModelSupported("gpt-5.3-codex-spark"))
	parent.TempUnschedulableUntil = &until
	require.False(t, parentHealthyForShadow(shadow, lookup), "shared credentials or proxy failures still block both")
	usage, err := (&AccountUsageService{}).getOpenAIUsage(context.Background(), shadow, true)
	require.NoError(t, err)
	require.Nil(t, usage.FiveHour)
	require.Nil(t, usage.SevenDay)
}
