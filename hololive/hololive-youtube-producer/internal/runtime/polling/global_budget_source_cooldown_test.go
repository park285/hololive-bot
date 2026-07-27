package polling

import (
	"context"
	"testing"
	"time"

	polling "github.com/kapu/hololive-shared/pkg/service/youtube/poller/runtime"
	sharedtestutil "github.com/kapu/hololive-shared/pkg/testutil"
	"github.com/stretchr/testify/require"
)

func TestGlobalBudgetLimiterMarkSourceCooldownDeniesSubsequentReserve(t *testing.T) {
	ctx := context.Background()
	cacheClient := sharedtestutil.NewTestCacheService(t, ctx)
	limiter := newTestGlobalBudgetLimiter(t, cacheClient, GlobalBudgetLimiterConfig{
		SourceMaxInflight: map[polling.BudgetSource]int{polling.BudgetSourceYouTubeScraper: 5},
	})

	reporter, ok := limiter.(polling.SourceCooldownReporter)
	require.True(t, ok)
	require.NoError(t, reporter.MarkSourceCooldown(ctx, polling.BudgetSourceYouTubeScraper, 5*time.Second, "test"))

	reservation, decision, err := limiter.TryReserve(ctx, testBudgetJob("cooldown-marked"), testBudgetProfile(polling.BudgetSourceYouTubeScraper, polling.BudgetBurstPrimary), time.Minute)
	require.NoError(t, err)
	require.Nil(t, reservation)
	require.False(t, decision.Allowed)
	require.Equal(t, "source_cooldown", decision.Reason)
	require.Equal(t, string(polling.BudgetSourceYouTubeScraper), decision.AffectedSource)
	require.Greater(t, decision.RetryAfter, time.Duration(0))
}

func TestGlobalBudgetLimiterDeniesLiveBatchFallbackDuringYouTubeCooldown(t *testing.T) {
	ctx := context.Background()
	cacheClient := sharedtestutil.NewTestCacheService(t, ctx)
	limiter := newTestGlobalBudgetLimiter(t, cacheClient, GlobalBudgetLimiterConfig{
		SourceMaxInflight: map[polling.BudgetSource]int{
			polling.BudgetSourceYouTubeScraper: 5,
			polling.BudgetSourceHolodexLive:    5,
		},
	})

	reporter, ok := limiter.(polling.SourceCooldownReporter)
	require.True(t, ok)
	require.NoError(t, reporter.MarkSourceCooldown(ctx, polling.BudgetSourceYouTubeScraper, 5*time.Second, "test"))

	reservation, decision, err := limiter.TryReserve(ctx, testBudgetJob("live-batch"), holodexLiveBatchBudgetProfile(30, polling.BudgetBurstPrimary, polling.BudgetPriorityHigh), time.Minute)

	require.NoError(t, err)
	require.False(t, decision.Allowed)
	require.Nil(t, reservation)
	require.Equal(t, "source_cooldown", decision.Reason)
	require.Equal(t, string(polling.BudgetSourceYouTubeScraper), decision.AffectedSource)
	require.Greater(t, decision.RetryAfter, time.Duration(0))
	require.Equal(t, 0, testInflightValue(t, ctx, cacheClient, testGlobalInflightKey(polling.BudgetSourceHolodexLive)))
	require.Equal(t, 0, testInflightValue(t, ctx, cacheClient, testGlobalInflightKey(polling.BudgetSourcePostgresWrite)))
}
