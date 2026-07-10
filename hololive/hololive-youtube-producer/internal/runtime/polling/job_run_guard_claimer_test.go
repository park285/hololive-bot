package polling

import (
	"context"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller"
	sharedtestutil "github.com/kapu/hololive-shared/pkg/testutil"
	"github.com/kapu/hololive-youtube-producer/internal/runtime/ingestionlease"
	"github.com/stretchr/testify/require"
)

var _ poller.JobClaimer = (*ingestionlease.JobRunGuard)(nil)

func TestBuildJobRunGuardClaimerRequiresCacheWhenActiveActiveEnabled(t *testing.T) {
	claimer, err := BuildJobRunGuardClaimer(nil, config.ScraperActiveActiveConfig{Enabled: true})

	require.Error(t, err)
	require.Nil(t, claimer)
}

func TestBuildJobRunGuardClaimerDisabledAllowsNilCache(t *testing.T) {
	claimer, err := BuildJobRunGuardClaimer(nil, config.ScraperActiveActiveConfig{})

	require.NoError(t, err)
	require.Nil(t, claimer)
}

func TestBuildJobRunGuardClaimerReturnsPollerClaims(t *testing.T) {
	ctx := context.Background()
	cache := sharedtestutil.NewTestCacheService(t, ctx)
	claimer, err := BuildJobRunGuardClaimer(cache, config.ScraperActiveActiveConfig{
		Enabled:    true,
		Namespace:  "test",
		InstanceID: "ap-a",
	})
	require.NoError(t, err)
	require.NotNil(t, claimer)

	status, claim, err := claimer.TryClaim(ctx, "videos", "UC_A", testLeaseTTL, testCooldownTTL)

	require.NoError(t, err)
	require.Equal(t, "acquired", string(status.Result))
	require.NotNil(t, claim)
}

func TestBuildJobRunGuardClaimerPreservesDeferCapability(t *testing.T) {
	ctx := context.Background()
	cache := sharedtestutil.NewTestCacheService(t, ctx)
	claimer, err := BuildJobRunGuardClaimer(cache, config.ScraperActiveActiveConfig{
		Enabled:    true,
		Namespace:  "test",
		InstanceID: "ap-a",
	})
	require.NoError(t, err)
	require.NotNil(t, claimer)

	status, claim, err := claimer.TryClaim(ctx, "videos", "UC_DEFER", testLeaseTTL, testCooldownTTL)
	require.NoError(t, err)
	require.Equal(t, poller.JobClaimAcquired, status.Result)

	deferrer, ok := claim.(interface {
		Defer(context.Context, time.Duration) (bool, error)
	})
	require.True(t, ok, "jobRunGuardClaim must expose Defer for admission deferred polls")

	deferred, err := deferrer.Defer(ctx, 5*time.Second)
	require.NoError(t, err)
	require.True(t, deferred)

	status, peerClaim, err := claimer.TryClaim(ctx, "videos", "UC_DEFER", testLeaseTTL, testCooldownTTL)
	require.NoError(t, err)
	require.Equal(t, poller.JobClaimAlreadyCompleted, status.Result)
	require.Nil(t, peerClaim)
	require.Greater(t, status.RetryAfter, time.Duration(0))
}

const (
	testLeaseTTL    = 60000000000
	testCooldownTTL = 60000000000
)
