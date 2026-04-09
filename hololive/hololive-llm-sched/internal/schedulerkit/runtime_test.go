package schedulerkit

import (
	"context"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestRuntimeSetClockIgnoresNil(t *testing.T) {
	rt := NewRuntime()
	want := time.Date(2026, 4, 9, 9, 0, 0, 0, time.UTC)

	rt.SetClock(func() time.Time { return want })
	rt.SetClock(nil)

	if got := rt.Now(); !got.Equal(want) {
		t.Fatalf("Now() = %s, want %s", got, want)
	}
}

func TestRuntimeStartIsGuarded(t *testing.T) {
	rt := NewRuntime()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	release := make(chan struct{})
	secondRun := make(chan struct{}, 1)
	var calls atomic.Int32

	rt.Start(ctx, Config{
		Logger:         testLogger(),
		WaitingLog:     "waiting",
		ContextStopLog: "context stop",
		StopLog:        "stop",
		CalculateNextRun: func(time.Time) time.Time {
			return time.Now().Add(-time.Millisecond)
		},
		OnTick: func(context.Context) {
			if calls.Add(1) > 1 {
				select {
				case secondRun <- struct{}{}:
				default:
				}
			}
			<-release
		},
	})
	rt.Start(ctx, Config{
		Logger:         testLogger(),
		WaitingLog:     "waiting",
		ContextStopLog: "context stop",
		StopLog:        "stop",
		CalculateNextRun: func(time.Time) time.Time {
			return time.Now().Add(-time.Millisecond)
		},
		OnTick: func(context.Context) {
			if calls.Add(1) > 1 {
				select {
				case secondRun <- struct{}{}:
				default:
				}
			}
			<-release
		},
	})

	time.Sleep(25 * time.Millisecond)

	select {
	case <-secondRun:
		t.Fatal("expected guarded Start to prevent a second loop")
	default:
	}

	close(release)
	rt.Stop()
}

func TestRuntimeStopsOnContextCancellation(t *testing.T) {
	rt := NewRuntime()
	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{}, 1)
	tickCalled := make(chan struct{}, 1)

	rt.Start(ctx, Config{
		Logger:         testLogger(),
		WaitingLog:     "waiting",
		ContextStopLog: "context stop",
		StopLog:        "stop",
		CalculateNextRun: func(time.Time) time.Time {
			return time.Now().Add(time.Hour)
		},
		OnTick: func(context.Context) {
			tickCalled <- struct{}{}
		},
		OnStop: func(StopReason) {
			stopped <- struct{}{}
		},
	})

	cancel()

	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("expected runtime to stop after context cancellation")
	}

	select {
	case <-tickCalled:
		t.Fatal("expected no tick before context cancellation")
	default:
	}

	rt.Stop()
}

func TestRuntimeCanRestartAfterStop(t *testing.T) {
	rt := NewRuntime()
	stopped := make(chan StopReason, 1)

	rt.Start(context.Background(), Config{
		Logger:         testLogger(),
		WaitingLog:     "waiting",
		ContextStopLog: "context stop",
		StopLog:        "stop",
		CalculateNextRun: func(time.Time) time.Time {
			return time.Now().Add(time.Hour)
		},
		OnTick: func(context.Context) {
			t.Fatal("expected Stop to happen before any tick")
		},
		OnStop: func(reason StopReason) {
			stopped <- reason
		},
	})

	rt.Stop()

	select {
	case reason := <-stopped:
		if reason != StopReasonManual {
			t.Fatalf("Stop reason = %v, want %v", reason, StopReasonManual)
		}
	case <-time.After(time.Second):
		t.Fatal("expected first runtime to stop")
	}

	ctx, cancel := context.WithCancel(context.Background())
	tickCalled := make(chan struct{}, 1)

	rt.Start(ctx, Config{
		Logger:         testLogger(),
		WaitingLog:     "waiting",
		ContextStopLog: "context stop",
		StopLog:        "stop",
		CalculateNextRun: func(time.Time) time.Time {
			return time.Now().Add(-time.Millisecond)
		},
		OnTick: func(context.Context) {
			select {
			case tickCalled <- struct{}{}:
			default:
			}
			cancel()
		},
	})

	select {
	case <-tickCalled:
	case <-time.After(time.Second):
		t.Fatal("expected restarted runtime to tick")
	}

	rt.Stop()
}

func TestRuntimeCanRestartAfterContextCancellation(t *testing.T) {
	rt := NewRuntime()
	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan StopReason, 1)

	rt.Start(ctx, Config{
		Logger:         testLogger(),
		WaitingLog:     "waiting",
		ContextStopLog: "context stop",
		StopLog:        "stop",
		CalculateNextRun: func(time.Time) time.Time {
			return time.Now().Add(time.Hour)
		},
		OnTick: func(context.Context) {
			t.Fatal("expected cancellation before any tick")
		},
		OnStop: func(reason StopReason) {
			stopped <- reason
		},
	})

	cancel()

	select {
	case reason := <-stopped:
		if reason != StopReasonContextCancelled {
			t.Fatalf("Stop reason = %v, want %v", reason, StopReasonContextCancelled)
		}
	case <-time.After(time.Second):
		t.Fatal("expected runtime to stop after context cancellation")
	}

	ctx2, cancel2 := context.WithCancel(context.Background())
	tickCalled := make(chan struct{}, 1)

	rt.Start(ctx2, Config{
		Logger:         testLogger(),
		WaitingLog:     "waiting",
		ContextStopLog: "context stop",
		StopLog:        "stop",
		CalculateNextRun: func(time.Time) time.Time {
			return time.Now().Add(-time.Millisecond)
		},
		OnTick: func(context.Context) {
			select {
			case tickCalled <- struct{}{}:
			default:
			}
			cancel2()
		},
	})

	select {
	case <-tickCalled:
	case <-time.After(time.Second):
		t.Fatal("expected runtime to restart after context cancellation")
	}

	rt.Stop()
}
