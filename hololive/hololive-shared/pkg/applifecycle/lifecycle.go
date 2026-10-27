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

package applifecycle

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"

	"github.com/park285/shared-go/v2/pkg/runtime/lifecycle"

	"github.com/kapu/hololive-shared/pkg/constants"
)

// StartHooks는 runtime 공통 시작 단계(HTTP 시작)만 담는다. background 작업의 시작·취소·대기는 각 runtime이
// 직접 소유한다.
type StartHooks struct {
	Logger          *slog.Logger
	ServerAddr      string
	StartHTTPServer func(errCh chan<- error)
}

// ShutdownHooks는 runtime 공통 종료 단계(HTTP 종료)만 담는다. 고유 종료 단계는
// 각 runtime이 Shutdown 반환 뒤 이어서 실행하고 오류를 직접 합친다.
type ShutdownHooks struct {
	Logger             *slog.Logger
	ShutdownHTTPServer func(ctx context.Context) error
}

func Start(_ context.Context, errCh chan<- error, hooks StartHooks) {
	if hooks.StartHTTPServer != nil {
		hooks.StartHTTPServer(errCh)
	}

	if hooks.Logger != nil && hooks.ServerAddr != "" {
		hooks.Logger.Info("HTTP server started", slog.String("addr", hooks.ServerAddr))
	}
}

func Run(logger *slog.Logger, start func(context.Context, chan<- error), shutdown func(context.Context) error) error {
	var runtimeErr error

	opts := runtimeOptions(logger, start, shutdown)
	onError := opts.OnError

	opts.OnError = func(err error) {
		runtimeErr = err
		onError(err)
	}

	err := lifecycle.Run(context.Background(), opts)

	if logger != nil {
		logger.Info("Shutdown complete")
	}

	if runtimeErr != nil && !errors.Is(err, runtimeErr) {
		return errors.Join(runtimeErr, err)
	}

	if err != nil {
		return fmt.Errorf("run lifecycle: %w", err)
	}

	return nil
}

func runtimeOptions(logger *slog.Logger, start func(context.Context, chan<- error), shutdown func(context.Context) error) lifecycle.Options {
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
			if err := shutdown(ctx); err != nil {
				logError(logger, "Shutdown error", err)

				return fmt.Errorf("shutdown: %w", err)
			}

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

func Shutdown(ctx context.Context, hooks ShutdownHooks) error {
	return shutdownHTTPServer(ctx, hooks)
}

func shutdownHTTPServer(ctx context.Context, hooks ShutdownHooks) error {
	if hooks.ShutdownHTTPServer != nil {
		if err := hooks.ShutdownHTTPServer(ctx); err != nil {
			logError(hooks.Logger, "HTTP server shutdown error", err)

			return fmt.Errorf("shutdown HTTP server: %w", err)
		}
	}

	return nil
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
