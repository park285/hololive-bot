package polling

import (
	"context"
	"testing"

	"github.com/kapu/hololive-shared/pkg/config"
	sharedtestutil "github.com/kapu/hololive-shared/pkg/testutil"
	"github.com/stretchr/testify/require"
)

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

func TestBuildJobRunGuardClaimerMapsClaims(t *testing.T) {
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

const (
	testLeaseTTL    = 60000000000
	testCooldownTTL = 60000000000
)
