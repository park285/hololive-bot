package producerruntime

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/service/cache"
	sharedtestutil "github.com/kapu/hololive-shared/pkg/testutil"
	"github.com/kapu/hololive-youtube-producer/internal/runtime/ingestionlease"
	"github.com/stretchr/testify/require"
)

type blockingPhotoSyncService struct {
	started chan struct{}
	once    sync.Once
}

func newBlockingPhotoSyncService() *blockingPhotoSyncService {
	return &blockingPhotoSyncService{started: make(chan struct{})}
}

func (s *blockingPhotoSyncService) Start(ctx context.Context) {
	s.once.Do(func() {
		close(s.started)
	})
	<-ctx.Done()
}

func TestLeasedPhotoSyncServiceAllowsOnlyOneOwner(t *testing.T) {
	ctx := t.Context()

	cacheService := sharedtestutil.NewTestCacheService(t, ctx)
	started := atomic.Int32{}
	serviceA := countedPhotoSyncService(newBlockingPhotoSyncService(), &started)
	serviceB := countedPhotoSyncService(newBlockingPhotoSyncService(), &started)

	go testLeasedPhotoSyncService(cacheService, "ap-a", serviceA).Start(ctx)
	go testLeasedPhotoSyncService(cacheService, "ap-b", serviceB).Start(ctx)

	require.Eventually(t, func() bool {
		return started.Load() == 1
	}, time.Second, 10*time.Millisecond)
	require.Never(t, func() bool {
		return started.Load() > 1
	}, 120*time.Millisecond, 10*time.Millisecond)
}

func testLeasedPhotoSyncService(
	cacheClient cache.Client,
	instanceID string,
	inner photoSyncService,
) *leasedPhotoSyncService {
	return &leasedPhotoSyncService{
		inner: inner,
		guard: ingestionlease.NewJobRunGuard(cacheClient, ingestionlease.JobRunGuardConfig{
			Namespace:  "test",
			InstanceID: instanceID,
		}),
		leaseTTL:      300 * time.Millisecond,
		retryInterval: 30 * time.Millisecond,
	}
}

func countedPhotoSyncService(inner *blockingPhotoSyncService, counter *atomic.Int32) photoSyncService {
	return &countingPhotoSyncService{inner: inner, counter: counter}
}

type countingPhotoSyncService struct {
	inner   *blockingPhotoSyncService
	counter *atomic.Int32
}

func (s *countingPhotoSyncService) Start(ctx context.Context) {
	s.counter.Add(1)
	s.inner.Start(ctx)
}
