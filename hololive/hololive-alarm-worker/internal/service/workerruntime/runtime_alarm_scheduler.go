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

package workerruntime

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/park285/shared-go/v2/pkg/panicguard"

	"github.com/kapu/hololive-shared/pkg/panicscope"
)

// alarmSchedulerHooks는 alarm-worker background scheduler 묶음의 시작 함수와 취소 함수 등록이다.
// 취소·종료 대기는 AlarmWorkerRuntime(schedulerCancel·schedulerDone)이 소유한다.
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

	// 중간 원인의 취소 sentinel만으로 scheduler의 실제 말단 오류를 무시하지 않습니다.
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
