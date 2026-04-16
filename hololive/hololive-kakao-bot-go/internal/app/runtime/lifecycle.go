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
	"log/slog"
	"os"

	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/runtime/lifecycle"
)

type StartHooks struct {
	Logger                  *slog.Logger
	ServerAddr              string
	StartAlarmScheduler     func(ctx context.Context)
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

	startAlarmScheduler(ctx, hooks)

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
	if err := lifecycle.Run(lifecycle.Options{
		ShutdownTimeout: constants.AppTimeout.Shutdown,
		Start: func(ctx context.Context, errCh chan<- error) {
			start(ctx, errCh)
			if logger != nil {
				logger.Info("Runtime started, waiting for signals...")
			}
		},
		OnSignal: func(sig os.Signal) {
			if logger != nil {
				logger.Info("Received shutdown signal", slog.String("signal", sig.String()))
			}
		},
		OnError: func(err error) {
			if logger != nil {
				logger.Error("Server error", slog.Any("error", err))
			}
		},
		BeforeShutdown: func() {
			if logger != nil {
				logger.Info("Shutting down gracefully...")
			}
		},
		Shutdown: func(ctx context.Context) error {
			shutdown(ctx)
			return nil
		},
	}); err != nil {
		logError(logger, "Shutdown error", err)
	}

	if logger != nil {
		logger.Info("Shutdown complete")
	}
}

func Shutdown(ctx context.Context, hooks ShutdownHooks) {
	if hooks.ClearAlarmScheduler != nil && hooks.ClearAlarmScheduler() {
		logInfo(hooks.Logger, "Alarm runtime scheduler cancellation signaled")
	}

	if hooks.ShutdownHTTPServer != nil {
		if err := hooks.ShutdownHTTPServer(ctx); err != nil {
			logError(hooks.Logger, "HTTP server shutdown error", err)
		}
	}

	if hooks.WebhookHandlerClose != nil {
		if err := hooks.WebhookHandlerClose(); err != nil {
			logError(hooks.Logger, "Iris webhook handler shutdown error", err)
		} else {
			logInfo(hooks.Logger, "Iris webhook handler stopped")
		}
	}

	if hooks.ShutdownAlarmServices != nil {
		if err := hooks.ShutdownAlarmServices(ctx); err != nil {
			logError(hooks.Logger, "Alarm service shutdown error", err)
		} else {
			logInfo(hooks.Logger, "Alarm services stopped")
		}
	}

	if hooks.ShutdownBot != nil {
		if err := hooks.ShutdownBot(ctx); err != nil {
			logError(hooks.Logger, "Error during shutdown", err)
		}
	}
}

func startAlarmScheduler(ctx context.Context, hooks StartHooks) {
	if hooks.StartAlarmScheduler == nil {
		logInfo(hooks.Logger, "Alarm runtime scheduler not configured")
		return
	}

	if hooks.SetAlarmSchedulerCancel == nil {
		go hooks.StartAlarmScheduler(ctx)
		logInfo(hooks.Logger, "Alarm runtime scheduler started")
		return
	}

	alarmCtx, alarmCancel := context.WithCancel(ctx)
	hooks.SetAlarmSchedulerCancel(alarmCancel)

	go hooks.StartAlarmScheduler(alarmCtx)
	logInfo(hooks.Logger, "Alarm runtime scheduler started")
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
