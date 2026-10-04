//go:build integration

package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestPrismFallbackQuotaCandidatesIntegration(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	client := tx.Client()
	repo := newAccountRepositoryWithSQL(client, tx, nil)
	future := time.Now().Add(time.Hour)
	group := createEntGroup(t, ctx, client, "prism-fallback-group")
	for _, tc := range []struct {
		name     string
		typeName string
		extra    map[string]any
		eligible bool
	}{
		{"fallback", service.AccountTypeOAuth, map[string]any{"openai_prism_fallback": true}, true},
		{"normal", service.AccountTypeOAuth, map[string]any{}, false},
		{"unchecked", service.AccountTypeOAuth, map[string]any{"openai_prism_fallback": false}, false},
		{"direct", service.AccountTypeOAuth, map[string]any{"openai_prism_fallback": true, "openai_prism_browser": true}, false},
		{"apikey", service.AccountTypeAPIKey, map[string]any{"openai_prism_fallback": true}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := mustCreateAccount(t, client, &service.Account{Name: "prism-" + tc.name, Platform: service.PlatformOpenAI,
				Type: tc.typeName, Extra: tc.extra, Status: service.StatusActive, Schedulable: true})
			_, err := client.Account.UpdateOneID(a.ID).SetRateLimitResetAt(future).Save(ctx)
			require.NoError(t, err)
			for _, list := range []func(context.Context) ([]service.Account, error){
				repo.ListSchedulable,
				func(ctx context.Context) ([]service.Account, error) {
					return repo.ListSchedulableByPlatform(ctx, service.PlatformOpenAI)
				},
				func(ctx context.Context) ([]service.Account, error) {
					return repo.ListSchedulableUngroupedByPlatform(ctx, service.PlatformOpenAI)
				},
			} {
				accounts, err := list(ctx)
				require.NoError(t, err)
				found := false
				for _, candidate := range accounts {
					if candidate.ID == a.ID {
						found = true
					}
				}
				require.Equal(t, tc.eligible, found)
			}
			_, err = client.AccountGroup.Create().SetAccountID(a.ID).SetGroupID(group.ID).Save(ctx)
			require.NoError(t, err)
			accounts, err := repo.ListSchedulableByGroupIDAndPlatform(ctx, group.ID, service.PlatformOpenAI)
			require.NoError(t, err)
			found := false
			for _, candidate := range accounts {
				if candidate.ID == a.ID {
					found = true
				}
			}
			require.Equal(t, tc.eligible, found)
		})
	}
}
