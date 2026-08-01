package producerruntime

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	sharedtestutil "github.com/kapu/hololive-shared/pkg/testutil"
	"github.com/park285/shared-go/pkg/runtime/lifecycle"
)

func TestYouTubeProducerRuntimeJoinsBackgroundServicesBeforeCleanup(t *testing.T) {
	started := make(chan struct{})
	canceled := make(chan struct{})
	release := make(chan struct{})
	var cleanupCalls atomic.Int64
	runtime := &YouTubeProducerRuntime{
		Logger: testLogger(),
		runActiveActiveRecovery: func(ctx context.Context) {
			close(started)
			<-ctx.Done()
			close(canceled)
			<-release
		},
		Managed: lifecycle.NewManaged(func() { cleanupCalls.Add(1) }),
	}

	runCtx, cancelRun := context.WithCancel(context.Background())
	runtime.startBackgroundServices(runCtx, make(chan error, 1))
	<-started
	cancelRun()
	<-canceled

	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), time.Second)
	defer cancelShutdown()
	shutdownDone := make(chan error, 1)
	go func() { shutdownDone <- runtime.shutdownRuntime(shutdownCtx) }()

	select {
	case err := <-shutdownDone:
		t.Fatalf("shutdown returned before background service exit: %v", err)
	case <-time.After(25 * time.Millisecond):
	}
	runtime.Close()
	if got := cleanupCalls.Load(); got != 0 {
		t.Fatalf("cleanup calls while background service active = %d, want 0", got)
	}

	close(release)
	if err := <-shutdownDone; err != nil {
		t.Fatalf("shutdown error = %v", err)
	}
	runtime.Close()
	runtime.Close()
	if got := cleanupCalls.Load(); got != 1 {
		t.Fatalf("cleanup calls after background service exit = %d, want 1", got)
	}
}

func TestYouTubeProducerRuntimeSkipsCleanupAfterBackgroundJoinTimeout(t *testing.T) {
	release := make(chan struct{})
	var cleanupCalls atomic.Int64
	runtime := &YouTubeProducerRuntime{
		Logger:  testLogger(),
		Managed: lifecycle.NewManaged(func() { cleanupCalls.Add(1) }),
	}
	runtime.startBackgroundService("blocked-test-service", func() { <-release })

	shutdownCtx, cancelShutdown := context.WithCancel(context.Background())
	cancelShutdown()
	if err := runtime.waitForBackgroundServices(shutdownCtx); err == nil {
		t.Fatal("waitForBackgroundServices error = nil, want canceled context")
	}
	runtime.Close()
	if got := cleanupCalls.Load(); got != 0 {
		t.Fatalf("cleanup calls after join timeout = %d, want 0", got)
	}

	close(release)
	deadline := time.Now().Add(time.Second)
	for runtime.activeBackgrounds.Load() != 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	runtime.Close()
	if got := cleanupCalls.Load(); got != 1 {
		t.Fatalf("cleanup calls after delayed exit = %d, want 1", got)
	}
}

func TestYouTubeProducerRuntimeKeepsDependenciesWhileTimedOutPhotoSyncIsRunning(t *testing.T) {
	cacheService := sharedtestutil.NewTestCacheService(t, t.Context())
	inner := newDelayedStopPhotoSyncService()
	t.Cleanup(inner.stop)
	photoSync := testLeasedPhotoSyncService(cacheService, "ap-a", inner)
	photoSync.leaseTTL = 2 * time.Second
	photoSync.shutdownTimeout = 20 * time.Millisecond

	var cleanupCalls atomic.Int64
	runtime := &YouTubeProducerRuntime{
		Logger:    testLogger(),
		PhotoSync: photoSync,
		Managed:   lifecycle.NewManaged(func() { cleanupCalls.Add(1) }),
	}
	runCtx, cancelRun := context.WithCancel(context.Background())
	runtime.startBackgroundServices(runCtx, make(chan error, 1))
	<-inner.started
	cancelRun()
	<-inner.cancelObserved

	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 2*photoSync.shutdownTimeout)
	defer cancelShutdown()
	if err := runtime.shutdownRuntime(shutdownCtx); err == nil {
		t.Fatal("shutdown error = nil, want background join timeout")
	}
	runtime.Close()
	if got := cleanupCalls.Load(); got != 0 {
		t.Fatalf("cleanup calls while timed-out photo sync is active = %d, want 0", got)
	}

	inner.stop()
	deadline := time.Now().Add(time.Second)
	for runtime.activeBackgrounds.Load() != 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	runtime.Close()
	if got := cleanupCalls.Load(); got != 1 {
		t.Fatalf("cleanup calls after timed-out photo sync exits = %d, want 1", got)
	}
}
