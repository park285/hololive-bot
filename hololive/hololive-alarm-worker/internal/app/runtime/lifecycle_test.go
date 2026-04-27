package runtime

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"
)

func TestStart_AlarmSchedulerErrorPropagatesToErrCh(t *testing.T) {
	t.Parallel()

	errCh := make(chan error, 1)
	schedulerCrash := errors.New("scheduler crashed")

	Start(t.Context(), errCh, StartHooks{
		Logger: slog.New(slog.DiscardHandler),
		StartAlarmScheduler: func(context.Context) error {
			return schedulerCrash
		},
	})

	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("expected scheduler error, got nil")
		}
		if !errors.Is(err, schedulerCrash) {
			t.Fatalf("unexpected scheduler error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("expected scheduler error to be sent to errCh")
	}
}

func TestStart_AlarmSchedulerContextCancellationIsNotFatal(t *testing.T) {
	t.Parallel()

	errCh := make(chan error, 1)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	Start(ctx, errCh, StartHooks{
		Logger: slog.New(slog.DiscardHandler),
		StartAlarmScheduler: func(context.Context) error {
			return context.Canceled
		},
	})

	select {
	case err := <-errCh:
		t.Fatalf("context cancellation must not be propagated as fatal error: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
}
