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

	applifecycle "github.com/kapu/hololive-shared/pkg/applifecycle"
)

// Shutdown은 설정 구독을 포함한 백그라운드 작업 종료를 ctx로 기다린 뒤 HTTP 서버와 알람 서비스를 정리합니다.
// Scheduler 대기 또는 cleanup 중 발생한 오류는 모두 결합해 반환합니다.
// 종료 context가 만료되어도 각 cleanup을 생략하지 않고 같은 context로 호출합니다.
func (r *AlarmWorkerRuntime) Shutdown(ctx context.Context) error {
	if r == nil {
		return nil
	}

	var schedulerWaitErr error

	shutdownErr := applifecycle.Shutdown(ctx, applifecycle.ShutdownHooks{
		Logger: r.Logger,
		ClearAlarmScheduler: func() bool {
			canceled := r.clearAlarmSchedulerCancel()

			schedulerWaitErr = r.waitAlarmScheduler(ctx)

			return canceled
		},
		ShutdownHTTPServer: r.ShutdownHTTPServer,
		ShutdownAlarmServices: func(ctx context.Context) error {
			if r.AlarmService == nil {
				return nil
			}

			return r.AlarmService.Close(ctx)
		},
	})

	if schedulerWaitErr != nil {
		shutdownErr = errors.Join(shutdownErr, fmt.Errorf("wait alarm scheduler: %w", schedulerWaitErr))
	}

	if shutdownErr != nil {
		return fmt.Errorf("shutdown: %w", shutdownErr)
	}

	return nil
}

func (r *AlarmWorkerRuntime) ShutdownHTTPServer(ctx context.Context) error {
	if r == nil {
		return nil
	}

	if err := r.HTTPServers.Shutdown(ctx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}

	return nil
}
