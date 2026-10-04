//go:build integration

package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestPrismShadowCredentialDimensionsIntegration(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	client := tx.Client()
	repo := newAccountRepositoryWithSQL(client, tx, nil)
	parent := mustCreateAccount(t, client, &service.Account{Name: "prism-parent", Platform: service.PlatformOpenAI,
		Type: service.AccountTypeOAuth, Status: service.StatusActive, Schedulable: true})
	for _, dimension := range []string{service.QuotaDimensionSpark, service.QuotaDimensionPrism} {
		shadow := &service.Account{Name: "shadow-" + dimension, Platform: service.PlatformOpenAI,
			Type: service.AccountTypeOAuth, Status: service.StatusActive, Schedulable: true,
			ParentAccountID: &parent.ID, QuotaDimension: dimension}
		require.NoError(t, repo.Create(ctx, shadow))
	}
	shadows, err := repo.ListShadowsByParent(ctx, parent.ID)
	require.NoError(t, err)
	require.Len(t, shadows, 2)
	_, err = client.Account.UpdateOneID(parent.ID).SetRateLimitResetAt(time.Now().Add(time.Hour)).Save(ctx)
	require.NoError(t, err)
	candidates, err := repo.ListSchedulableByPlatform(ctx, service.PlatformOpenAI)
	require.NoError(t, err)
	for _, shadow := range shadows {
		found := false
		for _, candidate := range candidates {
			if candidate.ID == shadow.ID {
				found = true
				if shadow.QuotaDimension == service.QuotaDimensionPrism {
					require.True(t, candidate.IsPrismShadow())
				}
			}
		}
		require.True(t, found, "parent OpenAI cooldown must not remove the shadow")
	}
}
