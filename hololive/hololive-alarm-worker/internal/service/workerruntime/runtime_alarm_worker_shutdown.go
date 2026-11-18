package workerruntime

import (
	"context"
	"errors"
	"fmt"

	applifecycle "github.com/kapu/hololive-shared/pkg/applifecycle"
)

func (r *AlarmWorkerRuntime) Shutdown(ctx context.Context) error {
	if r == nil {
		return nil
	}

	if r.clearAlarmSchedulerCancel() {
		logInfo(r.Logger, "Alarm runtime scheduler cancellation signaled")
	}

	schedulerWaitErr := r.waitAlarmScheduler(ctx)

	shutdownErr := applifecycle.Shutdown(ctx, applifecycle.ShutdownHooks{
		Logger:             r.Logger,
		ShutdownHTTPServer: r.ShutdownHTTPServer,
	})

	if schedulerWaitErr != nil {
		shutdownErr = errors.Join(shutdownErr, fmt.Errorf("wait alarm scheduler: %w", schedulerWaitErr))
	}

	if shutdownErr != nil {
		return fmt.Errorf("shut down alarm worker: %w", shutdownErr)
	}

	return nil
}

func (r *AlarmWorkerRuntime) ShutdownHTTPServer(ctx context.Context) error {
	if r == nil {
		return nil
	}

	if err := r.HTTPServers.Shutdown(ctx); err != nil {
		return fmt.Errorf("shut down HTTP servers: %w", err)
	}

	return nil
}
