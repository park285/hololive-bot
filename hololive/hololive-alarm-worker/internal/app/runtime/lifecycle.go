// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package runtime

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"

	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/park285/shared-go/pkg/runtime/lifecycle"
)

type StartHooks struct {
	Logger                  *slog.Logger
	ServerAddr              string
	StartAlarmScheduler     func(ctx context.Context) error
	RunConfigSubscriber     func(ctx context.Context)
	StartBot                func(ctx context.Context) error
	StartHTTPServer         func(errCh chan<- error)
	SetAlarmSchedulerCancel func(cancel context.CancelFunc)
}

type ShutdownHooks struct {
	Logger                *slog.Logger
	ClearAlarmScheduler   func() bool
	ShutdownHTTPServer    func(ctx context.Context) error
	WebhookHandlerClose   func() error
	ShutdownAlarmServices func(ctx context.Context) error
	ShutdownBot           func(ctx context.Context) error
}

func Start(ctx context.Context, errCh chan<- error, hooks StartHooks) {
	if ctx == nil {
		ctx = context.Background()
	}

	startAlarmScheduler(ctx, errCh, hooks)

	if hooks.RunConfigSubscriber != nil {
		go hooks.RunConfigSubscriber(ctx)
		logInfo(hooks.Logger, "Config subscriber started")
	}

	startBot(ctx, hooks.Logger, hooks.StartBot)

	if hooks.StartHTTPServer != nil {
		hooks.StartHTTPServer(errCh)
	}

	if hooks.Logger != nil && hooks.ServerAddr != "" {
		hooks.Logger.Info("HTTP server started", slog.String("addr", hooks.ServerAddr))
	}
}

func Run(logger *slog.Logger, start func(context.Context, chan<- error), shutdown func(context.Context)) {
	if err := lifecycle.Run(runtimeOptions(logger, start, shutdown)); err != nil {
		logError(logger, "Shutdown error", err)
	}

	if logger != nil {
		logger.Info("Shutdown complete")
	}
}

func runtimeOptions(logger *slog.Logger, start func(context.Context, chan<- error), shutdown func(context.Context)) lifecycle.Options {
	return lifecycle.Options{
		ShutdownTimeout: constants.AppTimeout.Shutdown,
		Start: func(ctx context.Context, errCh chan<- error) {
			startRuntime(ctx, errCh, logger, start)
		},
		OnSignal: func(sig os.Signal) {
			logSignal(logger, sig)
		},
		OnError: func(err error) {
			logError(logger, "Server error", err)
		},
		BeforeShutdown: func() {
			logInfo(logger, "Shutting down gracefully...")
		},
		Shutdown: func(ctx context.Context) error {
			shutdown(ctx)
			return nil
		},
	}
}

func startRuntime(ctx context.Context, errCh chan<- error, logger *slog.Logger, start func(context.Context, chan<- error)) {
	start(ctx, errCh)
	if logger != nil {
		logger.Info("Runtime started, waiting for signals...")
	}
}

func logSignal(logger *slog.Logger, sig os.Signal) {
	if logger != nil {
		logger.Info("Received shutdown signal", slog.String("signal", sig.String()))
	}
}

func Shutdown(ctx context.Context, hooks ShutdownHooks) {
	shutdownAlarmScheduler(hooks)
	shutdownHTTPServer(ctx, hooks)
	closeWebhookHandler(hooks)
	shutdownAlarmServices(ctx, hooks)
	shutdownBot(ctx, hooks)
}

func shutdownAlarmScheduler(hooks ShutdownHooks) {
	if hooks.ClearAlarmScheduler != nil && hooks.ClearAlarmScheduler() {
		logInfo(hooks.Logger, "Alarm runtime scheduler cancellation signaled")
	}
}

func shutdownHTTPServer(ctx context.Context, hooks ShutdownHooks) {
	if hooks.ShutdownHTTPServer != nil {
		if err := hooks.ShutdownHTTPServer(ctx); err != nil {
			logError(hooks.Logger, "HTTP server shutdown error", err)
		}
	}
}

func closeWebhookHandler(hooks ShutdownHooks) {
	if hooks.WebhookHandlerClose != nil {
		if err := hooks.WebhookHandlerClose(); err != nil {
			logError(hooks.Logger, "Iris webhook handler shutdown error", err)
		} else {
			logInfo(hooks.Logger, "Iris webhook handler stopped")
		}
	}
}

func shutdownAlarmServices(ctx context.Context, hooks ShutdownHooks) {
	if hooks.ShutdownAlarmServices != nil {
		if err := hooks.ShutdownAlarmServices(ctx); err != nil {
			logError(hooks.Logger, "Alarm service shutdown error", err)
		} else {
			logInfo(hooks.Logger, "Alarm services stopped")
		}
	}
}

func shutdownBot(ctx context.Context, hooks ShutdownHooks) {
	if hooks.ShutdownBot != nil {
		if err := hooks.ShutdownBot(ctx); err != nil {
			logError(hooks.Logger, "Error during shutdown", err)
		}
	}
}

func startAlarmScheduler(ctx context.Context, errCh chan<- error, hooks StartHooks) {
	if hooks.StartAlarmScheduler == nil {
		logInfo(hooks.Logger, "Alarm runtime scheduler not configured")
		return
	}

	alarmCtx := alarmSchedulerContext(ctx, hooks)

	go func() {
		if err := hooks.StartAlarmScheduler(alarmCtx); err != nil {
			handleAlarmSchedulerError(err, errCh, hooks.Logger)
		}
	}()

	logInfo(hooks.Logger, "Alarm runtime scheduler started")
}

func alarmSchedulerContext(ctx context.Context, hooks StartHooks) context.Context {
	if hooks.SetAlarmSchedulerCancel == nil {
		return ctx
	}

	alarmCtx, alarmCancel := context.WithCancel(ctx)
	hooks.SetAlarmSchedulerCancel(alarmCancel)
	return alarmCtx
}

func handleAlarmSchedulerError(err error, errCh chan<- error, logger *slog.Logger) {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		logInfo(logger, "Alarm runtime scheduler stopped")
		return
	}

	wrapped := fmt.Errorf("alarm runtime scheduler error: %w", err)
	if errCh != nil {
		errCh <- wrapped
		return
	}

	logError(logger, "Alarm runtime scheduler error", wrapped)
}

func startBot(ctx context.Context, logger *slog.Logger, startBot func(ctx context.Context) error) {
	if startBot == nil {
		return
	}

	go func() {
		if err := startBot(ctx); err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				logInfo(logger, "Background runtime task stopped (context done)")
			} else {
				logError(logger, "Background runtime task error", err)
			}
		}
	}()
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
