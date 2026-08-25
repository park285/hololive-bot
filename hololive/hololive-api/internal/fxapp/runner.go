package fxapp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	sharedlogging "github.com/park285/shared-go/v2/pkg/logging"
	"go.uber.org/fx"
)

type runnerApplication interface {
	Start(context.Context) error
	Wait() <-chan fx.ShutdownSignal
	Stop(context.Context) error
	LifecycleTimeouts() (time.Duration, time.Duration)
	SafetyClose(context.Context)
}

func (a *Application) Run(logger *slog.Logger) int {
	return runApplication(a, logger)
}

func (a *Application) Start(ctx context.Context) error {
	if a == nil || a.app == nil {
		return errors.New("fx application must not be nil")
	}

	if err := a.app.Start(ctx); err != nil {
		return fmt.Errorf("start fx application: %w", err)
	}

	return nil
}

func (a *Application) Wait() <-chan fx.ShutdownSignal {
	if a == nil || a.app == nil {
		return nil
	}

	return a.app.Wait()
}

func (a *Application) Stop(ctx context.Context) error {
	if a == nil || a.app == nil {
		return errors.New("fx application must not be nil")
	}

	if err := a.app.Stop(ctx); err != nil {
		return fmt.Errorf("stop fx application: %w", err)
	}

	return nil
}

func (a *Application) StartTimeout() time.Duration {
	if a == nil || a.app == nil {
		return processLifecycleTimeout
	}

	return a.app.StartTimeout()
}

func (a *Application) StopTimeout() time.Duration {
	if a == nil || a.app == nil {
		return processLifecycleTimeout
	}

	return a.app.StopTimeout()
}

func (a *Application) LifecycleTimeouts() (time.Duration, time.Duration) {
	return a.StartTimeout(), a.StopTimeout()
}

func (a *Application) SafetyClose(ctx context.Context) {
	if a == nil {
		return
	}

	a.resources.Close(ctx)
}

func runApplication(application runnerApplication, logger *slog.Logger) int {
	if application == nil {
		logDiagnosticError(logger, "hololive-api Fx application is unavailable", errors.New("application is nil"))

		return 1
	}

	startTimeout, stopTimeout := application.LifecycleTimeouts()
	startCtx, startCancel := context.WithTimeout(context.Background(), positiveTimeout(startTimeout))
	startErr := application.Start(startCtx)

	startCancel()

	if startErr != nil {
		logDiagnosticError(logger, "hololive-api Fx start failed", startErr)

		closeCtx, closeCancel := context.WithTimeout(context.Background(), positiveTimeout(stopTimeout))
		application.SafetyClose(closeCtx)
		closeCancel()

		return 1
	}

	shutdownSignal := <-application.Wait()
	if shutdownSignal.Signal != nil && logger != nil {
		logger.Info("hololive-api shutdown signal received", slog.String("signal", shutdownSignal.Signal.String()))
	}

	stopCtx, stopCancel := context.WithTimeout(context.Background(), positiveTimeout(stopTimeout))
	stopErr := application.Stop(stopCtx)

	stopCancel()

	if stopErr != nil {
		if errors.Is(stopErr, context.DeadlineExceeded) {
			logDiagnosticError(logger, "hololive-api Fx stop timed out", stopErr)
		} else {
			logDiagnosticError(logger, "hololive-api Fx stop failed", stopErr)
		}

		return 1
	}

	if shutdownSignal.ExitCode != 0 {
		return 1
	}

	return 0
}

func positiveTimeout(timeout time.Duration) time.Duration {
	if timeout <= 0 {
		return processLifecycleTimeout
	}

	return timeout
}

func logDiagnosticError(logger *slog.Logger, message string, err error) {
	if logger == nil {
		logger = slog.Default()
	}

	safeError := "unknown error"

	if err != nil {
		safeError = sharedlogging.RedactDiagnostic(err.Error())
	}

	logger.Error(
		sharedlogging.RedactDiagnostic(message),
		slog.String("error", safeError),
	)
}
