//go:build unit

package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestSchedulerPrismFallbackSurvivesCandidateProjection(t *testing.T) {
	ctx := context.Background()
	cache := newSchedulerCacheUnit(t)
	until := time.Now().Add(time.Hour)
	account := &service.Account{ID: 717, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth,
		Status: service.StatusActive, Schedulable: true, RateLimitResetAt: &until,
		Extra: map[string]any{"openai_prism_fallback": true}}
	bucket := service.SchedulerBucket{Platform: service.PlatformOpenAI, Mode: service.SchedulerModeSingle}
	token, err := cache.CaptureBucketWriteToken(ctx, bucket)
	require.NoError(t, err)
	require.NoError(t, cache.SetSnapshot(ctx, bucket, token, []service.Account{*account}))
	accounts, hit, err := cache.GetSnapshot(ctx, bucket)
	require.NoError(t, err)
	require.True(t, hit)
	require.Len(t, accounts, 1)
	projection := accounts[0]
	require.True(t, projection.IsSchedulableForModel("gpt-6-astra"))
	require.False(t, projection.IsSchedulableForModel("gpt-5.4"))
}
