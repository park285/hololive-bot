package applifecycle

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSchedulerChildContextErrorReachesLiveParent(t *testing.T) {
	for _, childErr := range []error{context.DeadlineExceeded, context.Canceled} {
		t.Run(childErr.Error(), func(t *testing.T) {
			errCh := make(chan error, 1)
			startAlarmScheduler(t.Context(), errCh, StartHooks{StartAlarmScheduler: func(context.Context) error { return childErr }})

			select {
			case err := <-errCh:
				require.ErrorIs(t, err, childErr)
			case <-time.After(time.Second):
				t.Fatal("child context error was lost while parent was live")
			}
		})
	}
}

func TestSchedulerParentCancellationIsGraceful(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	errCh := make(chan error, 1)
	handleAlarmSchedulerError(ctx, context.Canceled, errCh, nil)

	select {
	case err := <-errCh:
		t.Fatalf("parent cancellation reported: %v", err)
	default:
	}
}

func TestSchedulerContextCancellationTreeIsGraceful(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{
			name: "single wrap",
			err:  fmt.Errorf("scheduler stopped: %w", context.Canceled),
		},
		{
			name: "joined context errors",
			err: errors.Join(
				context.Canceled,
				fmt.Errorf("scheduler deadline: %w", context.DeadlineExceeded),
			),
		},
		{
			name: "nested joined context errors",
			err: fmt.Errorf("scheduler stopped: %w", errors.Join(
				fmt.Errorf("cancel: %w", context.Canceled),
				errors.Join(context.DeadlineExceeded),
			)),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			cancel()

			errCh := make(chan error, 1)
			handleAlarmSchedulerError(ctx, tc.err, errCh, nil)

			select {
			case err := <-errCh:
				t.Fatalf("context-only scheduler error reported: %v", err)
			default:
			}
		})
	}
}

func TestSchedulerMixedErrorDuringParentShutdownPreservesCauseWithoutBlocking(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	schedulerErr := errors.New("scheduler failed after cancellation")
	mixedErr := fmt.Errorf("scheduler result: %w", errors.Join(
		fmt.Errorf("scheduler cancellation: %w", context.Canceled),
		errors.Join(fmt.Errorf("scheduler operation: %w", schedulerErr)),
	))

	var logs bytes.Buffer

	logger := slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelError}))
	done := make(chan struct{})

	go func() {
		handleAlarmSchedulerError(ctx, mixedErr, make(chan error), logger)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("mixed scheduler error delivery blocked after parent shutdown")
	}

	assert.Contains(t, logs.String(), "Alarm runtime scheduler error during shutdown")
	assert.Contains(t, logs.String(), schedulerErr.Error())
}

func TestSchedulerErrorDeliveryUnblocksOnParentShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})

	go func() {
		handleAlarmSchedulerError(ctx, context.DeadlineExceeded, make(chan error), nil)
		close(done)
	}()

	cancel()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("error delivery blocked after parent shutdown")
	}
}
