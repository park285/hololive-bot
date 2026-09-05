package youtubedispatch

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	dispatchstate "github.com/kapu/hololive-alarm-worker/internal/service/youtube/outbox/dispatchstate"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
)

func TestLogTickerStopOmitsContextStop(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer

	logger := slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelWarn}))

	logTickerStop(logger, "Outbox dispatcher loop stopped with error", context.Canceled)
	logTickerStop(logger, "Outbox dispatcher loop stopped with error", context.DeadlineExceeded)
	logTickerStop(logger, "Outbox dispatcher loop stopped with error", nil)

	if logs.Len() != 0 {
		t.Fatalf("context stop logged a warning: %s", logs.String())
	}
}

func TestLogTickerStopWarnsOnUnexpectedError(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer

	logger := slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelWarn}))

	logTickerStop(logger, "Outbox dispatcher loop stopped with error", errors.New("tick failed"))

	if !strings.Contains(logs.String(), `"level":"WARN"`) {
		t.Fatalf("unexpected error did not log a warning: %s", logs.String())
	}
}

func TestDispatcherRunDoesNotWarnWhenContextCanceled(t *testing.T) {
	t.Parallel()

	assertLoopCancelHasNoWarn(t, func(ctx context.Context, dispatcher *Dispatcher) {
		dispatcher.run(ctx)
	})
}

func TestDispatcherCleanupLoopDoesNotWarnWhenContextCanceled(t *testing.T) {
	t.Parallel()

	assertLoopCancelHasNoWarn(t, func(ctx context.Context, dispatcher *Dispatcher) {
		dispatcher.cleanupLoop(ctx)
	})
}

func TestDispatcherReviveLoopDoesNotWarnWhenContextCanceled(t *testing.T) {
	t.Parallel()

	assertLoopCancelHasNoWarn(t, func(ctx context.Context, dispatcher *Dispatcher) {
		dispatcher.reviveLoop(ctx)
	})
}

func TestDispatcherAggregateSyncLoopDoesNotWarnWhenContextCanceled(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer

	logger := slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelWarn}))
	dispatcher := newCanceledTickerDispatcher(t, logger)
	started := make(chan struct{})

	dispatcher.setOnAggregateSync(func() {
		select {
		case <-started:
		default:
			close(started)
		}
	})

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})

	go func() {
		dispatcher.aggregateSyncLoop(ctx)
		close(done)
	}()

	select {
	case <-started:
	case <-time.After(3 * time.Second):
		cancel()
		t.Fatal("aggregate sync loop did not start")
	}

	cancel()
	waitLoopStopWithoutWarn(t, done, &logs)
}

func TestDispatcherTelemetryLoopDoesNotWarnWhenContextCanceled(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer

	logger := slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelWarn}))
	dispatcher := newCanceledTickerDispatcher(t, logger)

	dispatcher.telemetry.telemetry = nil

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})

	go func() {
		dispatcher.telemetry.telemetryLoop(ctx)
		close(done)
	}()

	cancel()
	waitLoopStopWithoutWarn(t, done, &logs)
}

func newCanceledTickerDispatcher(t *testing.T, logger *slog.Logger) *Dispatcher {
	t.Helper()

	dispatcher := newDispatcherForTest(t, openDispatcherStartTestDB(t, "dispatcher_cancel_log"), cachemocks.NewLenientClient(), &testSender{failRoom: map[string]bool{}}, nil, logger, &dispatchstate.Config{
		BatchSize:             10,
		LockTimeout:           time.Minute,
		PollInterval:          time.Hour,
		AggregateSyncInterval: time.Hour,
		TelemetryPollInterval: time.Hour,
		ReviveInterval:        time.Hour,
		MaxRetries:            3,
		RetryBackoff:          time.Minute,
		CleanupEnabled:        true,
		ReviveEnabled:         true,
	})

	return dispatcher
}

func assertLoopCancelHasNoWarn(t *testing.T, run func(context.Context, *Dispatcher)) {
	t.Helper()

	var logs bytes.Buffer

	logger := slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelWarn}))
	dispatcher := newCanceledTickerDispatcher(t, logger)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})

	go func() {
		run(ctx, dispatcher)
		close(done)
	}()

	cancel()
	waitLoopStopWithoutWarn(t, done, &logs)
}

func waitLoopStopWithoutWarn(t *testing.T, done <-chan struct{}, logs *bytes.Buffer) {
	t.Helper()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("loop did not stop after context cancellation")
	}

	if strings.Contains(logs.String(), `"level":"WARN"`) {
		t.Fatalf("canceled ticker logged a warning: %s", logs.String())
	}
}
