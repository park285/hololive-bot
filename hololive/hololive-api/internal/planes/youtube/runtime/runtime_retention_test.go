package runtime

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/service/youtube/sourceobservation"
)

func TestShutdownJoinsRetentionAndReplayWorkers(t *testing.T) {
	var retentionTicks atomic.Int64
	var replayTicks atomic.Int64
	runtime := newTestRuntime(fakeClaimer{}, fakeConsumer{})
	runtime.Config.Retention.Enabled = true
	runtime.Config.Replay.Enabled = true
	runtime.retainer = fakeRetainer{
		tick: func(context.Context, sourceobservation.RetentionConfig, time.Time) (sourceobservation.RetentionResult, error) {
			retentionTicks.Add(1)
			return sourceobservation.RetentionResult{}, nil
		},
	}
	runtime.replayer = fakeReplayer{
		next: func(context.Context) (bool, error) {
			replayTicks.Add(1)
			return false, nil
		},
	}

	runtime.Start(context.Background(), make(chan error, 1))
	if runtime.loopCount != 5 {
		t.Fatalf("loopCount = %d, want 5", runtime.loopCount)
	}
	waitForTicks(t, &retentionTicks)
	waitForTicks(t, &replayTicks)
	if err := runtime.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
}

func TestRetentionAndReplayLoopsStayStoppedWhenDisabled(t *testing.T) {
	runtime := newTestRuntime(fakeClaimer{}, fakeConsumer{})
	runtime.Config.Retention.Enabled = false
	runtime.Config.Replay.Enabled = false
	runtime.Start(context.Background(), make(chan error, 1))
	if runtime.loopCount != 3 {
		t.Fatalf("loopCount = %d, want 3 with only claim/projection/live-end", runtime.loopCount)
	}
	if err := runtime.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
}

func waitForTicks(t *testing.T, ticks *atomic.Int64) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if ticks.Load() > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("worker tick did not run")
}

type fakeRetainer struct {
	tick func(context.Context, sourceobservation.RetentionConfig, time.Time) (sourceobservation.RetentionResult, error)
}

func (f fakeRetainer) RunRetentionTick(
	ctx context.Context,
	cfg sourceobservation.RetentionConfig,
	now time.Time,
) (sourceobservation.RetentionResult, error) {
	if f.tick == nil {
		return sourceobservation.RetentionResult{}, nil
	}
	return f.tick(ctx, cfg, now)
}

type fakeReplayer struct {
	next func(context.Context) (bool, error)
}

func (f fakeReplayer) ProcessNextReplay(ctx context.Context) (bool, error) {
	if f.next == nil {
		return false, nil
	}
	return f.next(ctx)
}
