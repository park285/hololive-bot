package producerruntime

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	sharedtestutil "github.com/kapu/hololive-shared/pkg/testutil"
	"github.com/kapu/hololive-youtube-producer/internal/runtime/ingestionlease"
)

func TestCleanupIngestionRuntimeStartupFailureReleasesLease(t *testing.T) {
	ctx := context.Background()
	cacheService := sharedtestutil.NewTestCacheService(t, ctx)
	lease, err := ingestionlease.Acquire(ctx, cacheService, "youtube-producer", testLogger())
	require.NoError(t, err)
	requireIngestionLeaseExists(t, ctx, cacheService, true)

	cleanupCalled := false
	state := ingestionRuntimeYouTubeState{ingestionLease: lease}
	cleanupIngestionRuntimeStartupFailure(ctx, &youtubeProducerInfrastructure{
		cleanup: func() {
			cleanupCalled = true
		},
	}, testLogger(), "youtube-producer", &state)

	require.True(t, cleanupCalled)
	require.Nil(t, state.ingestionLease)
	requireIngestionLeaseExists(t, ctx, cacheService, false)
	next, err := ingestionlease.Acquire(ctx, cacheService, "youtube-producer", testLogger())
	require.NoError(t, err)
	require.NoError(t, next.Release(ctx))
}

func requireIngestionLeaseExists(t *testing.T, ctx context.Context, cacheService interface {
	Exists(context.Context, string) (bool, error)
}, want bool) {
	t.Helper()

	exists, err := cacheService.Exists(ctx, ingestionlease.Key)
	require.NoError(t, err)
	require.Equal(t, want, exists)
}
