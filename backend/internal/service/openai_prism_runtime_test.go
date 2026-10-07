//go:build unit

package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestPrismTurnIdentitySurvivesAccountChangesAndRejectsPrivateOverrides(t *testing.T) {
	identity := func(keyID int64, session, body string) string {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
		c.Request.Header.Set("session-id", session)
		c.Request.Header.Set("X-Prism-Turn-ID", "forged")
		c.Set("api_key", &APIKey{ID: keyID})
		id, err := prismBrowserTurnID(c, []byte(body))
		require.NoError(t, err)
		return id
	}
	first := identity(7, "one", `{"model":"gpt-6.1-sol","input":"hello","stream":true}`)
	require.Len(t, first, 64)
	require.Equal(t, first, identity(7, "one", `{"stream":true,"input":"hello","model":"gpt-6.1-sol"}`))
	require.NotEqual(t, first, identity(8, "one", `{"model":"gpt-6.1-sol","input":"hello","stream":true}`))
	require.NotEqual(t, first, identity(7, "two", `{"model":"gpt-6.1-sol","input":"hello","stream":true}`))
	require.NotEqual(t, first, identity(7, "one", `{"model":"gpt-6.1-sol","input":"different","stream":true}`))
	require.Empty(t, identity(0, "one", `{"input":"hello"}`))
}

func TestPrismRuntimeDatabaseSwitchControlsDirectAndQuotaFallback(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		require.Equal(t, "Bearer database-key", r.Header.Get("Authorization"))
		_, _ = io.WriteString(w, `{"status":"completed"}`)
	}))
	defer server.Close()
	s, account := prismTestService("http://wrong.invalid")
	s.cfg.Gateway.PrismBrowser.Enabled = false
	repo := &settingRepoStub{values: map[string]string{
		SettingKeyPrismBrowserEnabled: "true", SettingKeyPrismBrowserBaseURL: server.URL + "/v1",
		SettingKeyPrismBrowserAPIKey: "database-key",
	}}
	s.settingService = &SettingService{settingRepo: repo}
	_, _, status, err := s.callPrismBrowser(context.Background(), account, []byte(`{"model":"gpt-6.1-sol","input":"hello"}`))
	require.NoError(t, err)
	require.Equal(t, 200, status)
	account.Extra = map[string]any{"openai_prism_fallback": true}
	require.True(t, s.prismFallbackEnabled(account, "gpt-6.1-sol"))
	_, _, _, err = s.callPrismBrowser(context.Background(), account, []byte(`{"model":"gpt-6.1-sol","input":"hello"}`))
	require.NoError(t, err)
	repo.values[SettingKeyPrismBrowserEnabled] = "false"
	require.False(t, s.prismFallbackEnabled(account, "gpt-6.1-sol"))
	_, _, _, err = s.callPrismBrowser(context.Background(), account, []byte(`{"model":"gpt-6.1-sol","input":"hello"}`))
	require.Error(t, err)
	repo.values[SettingKeyPrismBrowserEnabled] = "true"
	repo.err = errors.New("settings unavailable")
	require.False(t, s.prismFallbackEnabled(account, "gpt-6.1-sol"))
	require.Equal(t, 2, calls)
}

func TestPrismPublicRetryKeepsTurnHeaderAcrossAccounts(t *testing.T) {
	var headers []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		headers = append(headers, r.Header.Get("X-Prism-Turn-ID"))
		_, _ = io.WriteString(w, `{"id":"resp_test","status":"completed","model":"gpt-6.1-sol","output":[{"type":"message","content":[{"type":"output_text","text":"OK"}]}]}`)
	}))
	defer server.Close()
	s, account := prismTestService(server.URL)
	for _, accountID := range []int64{42, 43} {
		account.ID = accountID
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
		c.Request.Header.Set("X-Prism-Turn-ID", "untrusted")
		c.Set("api_key", &APIKey{ID: 7})
		_, err := s.forwardPrismBrowser(context.Background(), c, account, []byte(`{"model":"gpt-6.1-sol","input":"hello"}`), time.Now())
		require.NoError(t, err)
	}
	require.Len(t, headers, 2)
	require.Len(t, headers[0], 64)
	require.Equal(t, headers[0], headers[1])
}

func TestPrismLiveCatalogFiltersAliasesAndHonorsAccountScope(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		require.Equal(t, "/v1/models", r.URL.Path)
		require.NotEmpty(t, r.Header.Get("X-Prism-OAuth-Token"))
		require.NotEmpty(t, r.Header.Get("X-Prism-Account-ID"))
		_, _ = io.WriteString(w, `{"data":[{"id":"gpt-5.6-sol"},{"id":"gpt-6-luna"}]}`)
	}))
	defer server.Close()
	s, account := prismTestService(server.URL)
	models, err := s.fetchPrismBrowserModels(context.Background(), account)
	require.NoError(t, err)
	require.Len(t, models, 2)
	require.Equal(t, "gpt-5.6-sol", models[0].ID)
	account.Extra[PrismBrowserModelsKey] = []string{"gpt-6-luna"}
	models, err = s.fetchPrismBrowserModels(context.Background(), account)
	require.NoError(t, err)
	require.Len(t, models, 1)
	require.Equal(t, "gpt-6-luna", models[0].ID)
}

func TestPrismLiveCatalogFailureDoesNotInventStaticModels(t *testing.T) {
	for _, body := range []string{`{"data":[]}`, `{"error":"private-value"}`} {
		t.Run(body, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.WriteString(w, body)
			}))
			defer server.Close()
			s, account := prismTestService(server.URL)
			models, err := (&AccountTestService{openaiGatewayService: s}).FetchOpenAIAccountModels(context.Background(), account)
			require.Empty(t, models)
			if body == `{"data":[]}` {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				require.NotContains(t, err.Error(), "private-value")
			}
		})
	}
}
