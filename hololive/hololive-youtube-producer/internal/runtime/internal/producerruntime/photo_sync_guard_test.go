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

type delayedStopPhotoSyncService struct {
	started        chan struct{}
	cancelObserved chan struct{}
	allowStop      chan struct{}
	startOnce      sync.Once
	cancelOnce     sync.Once
	stopOnce       sync.Once
}

func newDelayedStopPhotoSyncService() *delayedStopPhotoSyncService {
	return &delayedStopPhotoSyncService{
		started:        make(chan struct{}),
		cancelObserved: make(chan struct{}),
		allowStop:      make(chan struct{}),
	}
}

func (s *delayedStopPhotoSyncService) Start(ctx context.Context) {
	s.startOnce.Do(func() { close(s.started) })
	<-ctx.Done()
	s.cancelOnce.Do(func() { close(s.cancelObserved) })
	<-s.allowStop
}

func (s *delayedStopPhotoSyncService) stop() {
	s.stopOnce.Do(func() { close(s.allowStop) })
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

func TestLeasedPhotoSyncServiceReleasesOnlyAfterInnerStops(t *testing.T) {
	cacheService := sharedtestutil.NewTestCacheService(t, t.Context())
	inner := newDelayedStopPhotoSyncService()
	t.Cleanup(inner.stop)
	service := testLeasedPhotoSyncService(cacheService, "ap-a", inner)
	service.leaseTTL = 2 * time.Second
	service.shutdownTimeout = time.Second

	ctx, cancel := context.WithCancel(context.Background())
	claim := acquirePhotoSyncClaim(t, ctx, service.guard, service.leaseTTL, service.retryInterval)
	runDone := make(chan bool, 1)
	go func() {
		runDone <- service.runOwned(ctx, claim)
	}()

	requireSignal(t, inner.started, "inner photo sync did not start")
	cancel()
	requireSignal(t, inner.cancelObserved, "inner photo sync did not observe cancellation")

	select {
	case <-runDone:
		t.Fatal("runOwned returned before the inner photo sync stopped")
	case <-time.After(50 * time.Millisecond):
	}

	contender := newPhotoSyncGuard(cacheService, "ap-b")
	status, contenderClaim, err := contender.TryLease(t.Context(), photoSyncIdentity(), service.leaseTTL, service.retryInterval)
	require.NoError(t, err)
	require.Equal(t, ingestionlease.JobClaimPeerOwned, status.Result)
	require.Nil(t, contenderClaim)

	inner.stop()
	select {
	case safeToContinue := <-runDone:
		require.True(t, safeToContinue)
	case <-time.After(time.Second):
		t.Fatal("runOwned did not finish after the inner photo sync stopped")
	}

	replacement := acquirePhotoSyncClaim(t, t.Context(), contender, service.leaseTTL, service.retryInterval)
	released, err := replacement.Release(t.Context())
	require.NoError(t, err)
	require.True(t, released)
}

func TestLeasedPhotoSyncServiceLeavesLeaseForTTLWhenInnerDoesNotStop(t *testing.T) {
	cacheService := sharedtestutil.NewTestCacheService(t, t.Context())
	inner := newDelayedStopPhotoSyncService()
	t.Cleanup(inner.stop)
	service := testLeasedPhotoSyncService(cacheService, "ap-a", inner)
	service.leaseTTL = 2 * time.Second
	service.shutdownTimeout = 40 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	claim := acquirePhotoSyncClaim(t, ctx, service.guard, service.leaseTTL, service.retryInterval)
	runDone := make(chan bool, 1)
	go func() {
		runDone <- service.runOwned(ctx, claim)
	}()

	requireSignal(t, inner.started, "inner photo sync did not start")
	cancel()
	requireSignal(t, inner.cancelObserved, "inner photo sync did not observe cancellation")

	select {
	case safeToContinue := <-runDone:
		require.False(t, safeToContinue)
	case <-time.After(time.Second):
		t.Fatal("runOwned did not return after the shutdown timeout")
	}

	contender := newPhotoSyncGuard(cacheService, "ap-b")
	status, contenderClaim, err := contender.TryLease(t.Context(), photoSyncIdentity(), service.leaseTTL, service.retryInterval)
	require.NoError(t, err)
	require.Equal(t, ingestionlease.JobClaimPeerOwned, status.Result)
	require.Nil(t, contenderClaim, "timed-out shutdown must not explicitly release the lease")
}

func testLeasedPhotoSyncService(
	cacheClient cache.Client,
	instanceID string,
	inner photoSyncService,
) *leasedPhotoSyncService {
	return &leasedPhotoSyncService{
		inner:         inner,
		guard:         newPhotoSyncGuard(cacheClient, instanceID),
		leaseTTL:      300 * time.Millisecond,
		retryInterval: 30 * time.Millisecond,
	}
}

func newPhotoSyncGuard(cacheClient cache.Client, instanceID string) *ingestionlease.JobRunGuard {
	return ingestionlease.NewJobRunGuard(cacheClient, ingestionlease.JobRunGuardConfig{
		Namespace:  "test",
		InstanceID: instanceID,
	})
}

func photoSyncIdentity() ingestionlease.JobIdentity {
	return ingestionlease.JobIdentity{
		PollerName: photoSyncLeasePollerName,
		ChannelID:  photoSyncLeaseChannelID,
	}
}

func acquirePhotoSyncClaim(
	t *testing.T,
	ctx context.Context,
	guard *ingestionlease.JobRunGuard,
	leaseTTL time.Duration,
	retryInterval time.Duration,
) *ingestionlease.JobRunClaim {
	t.Helper()
	status, claim, err := guard.TryLease(ctx, photoSyncIdentity(), leaseTTL, retryInterval)
	require.NoError(t, err)
	require.Equal(t, ingestionlease.JobClaimAcquired, status.Result)
	require.NotNil(t, claim)
	return claim
}

func requireSignal(t *testing.T, signal <-chan struct{}, failure string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(time.Second):
		t.Fatal(failure)
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
