package workerruntime

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/park285/shared-go/v2/pkg/panicguard"

	"github.com/kapu/hololive-shared/pkg/panicscope"
)

type alarmSchedulerHooks struct {
	Logger    *slog.Logger
	Start     func(ctx context.Context) error
	SetCancel func(cancel context.CancelFunc)
}

func startAlarmScheduler(ctx context.Context, errCh chan<- error, hooks alarmSchedulerHooks) {
	if hooks.Start == nil {
		return
	}

	alarmCtx := alarmSchedulerContext(ctx, hooks)

	go func() {
		if err := panicguard.RunE(hooks.Logger, panicscope.BackgroundTask, "alarm-scheduler", func() error {
			return hooks.Start(alarmCtx)
		}); err != nil {
			handleAlarmSchedulerError(alarmCtx, err, errCh, hooks.Logger)
		}
	}()

	logInfo(hooks.Logger, "Alarm runtime scheduler started")
}

func alarmSchedulerContext(ctx context.Context, hooks alarmSchedulerHooks) context.Context {
	if hooks.SetCancel == nil {
		return ctx
	}

	alarmCtx, alarmCancel := context.WithCancel(ctx)
	hooks.SetCancel(alarmCancel)

	return alarmCtx
}

func handleAlarmSchedulerError(ctx context.Context, err error, errCh chan<- error, logger *slog.Logger) {
	if ctx.Err() != nil && isContextTerminationTree(err) {
		logInfo(logger, "Alarm runtime scheduler stopped")

		return
	}

	wrapped := fmt.Errorf("alarm runtime scheduler error: %w", err)

	if errCh != nil {
		select {
		case errCh <- wrapped:
		case <-ctx.Done():
			logError(logger, "Alarm runtime scheduler error during shutdown", wrapped)
		}

		return
	}

	logError(logger, "Alarm runtime scheduler error", wrapped)
}

func isContextTerminationTree(err error) bool {
	if err == nil {
		return false
	}

	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		foundLeaf := false

		for _, child := range joined.Unwrap() {
			if child == nil {
				continue
			}

			foundLeaf = true

			if !isContextTerminationTree(child) {
				return false
			}
		}

		if foundLeaf {
			return true
		}
	}

	if unwrapped := errors.Unwrap(err); unwrapped != nil {
		return isContextTerminationTree(unwrapped)
	}

	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

func logInfo(logger *slog.Logger, msg string, attrs ...any) {
	if logger != nil {
		logger.Info(msg, attrs...)
	}
}

func logError(logger *slog.Logger, msg string, err error) {
	if logger != nil {
		logger.Error(msg, slog.Any("error", err))
	}
}
