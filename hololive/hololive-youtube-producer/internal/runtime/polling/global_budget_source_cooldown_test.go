package polling

import (
	"context"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/service/youtube/poller"
	sharedtestutil "github.com/kapu/hololive-shared/pkg/testutil"
	"github.com/stretchr/testify/require"
)

func TestGlobalBudgetLimiterMarkSourceCooldownDeniesSubsequentReserve(t *testing.T) {
	ctx := context.Background()
	cacheClient := sharedtestutil.NewTestCacheService(t, ctx)
	limiter := newTestGlobalBudgetLimiter(t, cacheClient, GlobalBudgetLimiterConfig{
		SourceMaxInflight: map[poller.BudgetSource]int{poller.BudgetSourceYouTubeScraper: 5},
	})

	reporter, ok := limiter.(poller.SourceCooldownReporter)
	require.True(t, ok)
	require.NoError(t, reporter.MarkSourceCooldown(ctx, poller.BudgetSourceYouTubeScraper, 5*time.Second, "test"))

	reservation, decision, err := limiter.TryReserve(ctx, testBudgetJob("cooldown-marked"), testBudgetProfile(poller.BudgetSourceYouTubeScraper, poller.BudgetBurstPrimary), time.Minute)
	require.NoError(t, err)
	require.Nil(t, reservation)
	require.False(t, decision.Allowed)
	require.Equal(t, "source_cooldown", decision.Reason)
	require.Equal(t, string(poller.BudgetSourceYouTubeScraper), decision.AffectedSource)
	require.Greater(t, decision.RetryAfter, time.Duration(0))
}

func TestGlobalBudgetLimiterDeniesLiveBatchFallbackDuringYouTubeCooldown(t *testing.T) {
	ctx := context.Background()
	cacheClient := sharedtestutil.NewTestCacheService(t, ctx)
	limiter := newTestGlobalBudgetLimiter(t, cacheClient, GlobalBudgetLimiterConfig{
		SourceMaxInflight: map[poller.BudgetSource]int{
			poller.BudgetSourceYouTubeScraper: 5,
			poller.BudgetSourceHolodexLive:    5,
		},
	})

	reporter, ok := limiter.(poller.SourceCooldownReporter)
	require.True(t, ok)
	require.NoError(t, reporter.MarkSourceCooldown(ctx, poller.BudgetSourceYouTubeScraper, 5*time.Second, "test"))

	reservation, decision, err := limiter.TryReserve(ctx, testBudgetJob("live-batch"), holodexLiveBatchBudgetProfile(30, poller.BudgetBurstPrimary, poller.BudgetPriorityHigh), time.Minute)

	require.NoError(t, err)
	require.False(t, decision.Allowed)
	require.Nil(t, reservation)
	require.Equal(t, "source_cooldown", decision.Reason)
	require.Equal(t, string(poller.BudgetSourceYouTubeScraper), decision.AffectedSource)
	require.Greater(t, decision.RetryAfter, time.Duration(0))
	require.Equal(t, 0, testInflightValue(t, ctx, cacheClient, testGlobalInflightKey(poller.BudgetSourceHolodexLive)))
	require.Equal(t, 0, testInflightValue(t, ctx, cacheClient, testGlobalInflightKey(poller.BudgetSourcePostgresWrite)))
}
