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
	"fmt"

	applifecycle "github.com/kapu/hololive-shared/pkg/applifecycle"
)

func (r *AlarmWorkerRuntime) Run() error {
	if r == nil {
		return nil
	}

	if err := applifecycle.Run(r.Logger, r.Start, r.Shutdown); err != nil {
		return fmt.Errorf("run: %w", err)
	}

	return nil
}

// Start는 스케줄러, egress와 설정 구독의 실행을 시작합니다.
// 시작한 백그라운드 작업의 취소와 종료 대기는 Shutdown이 소유합니다.
func (r *AlarmWorkerRuntime) Start(ctx context.Context, errCh chan<- error) {
	if r == nil {
		return
	}

	r.beginAlarmScheduler()

	if r.WorkerObservability != nil {
		r.WorkerObservability.Start(ctx)
	}

	applifecycle.Start(ctx, errCh, applifecycle.StartHooks{
		Logger:     r.Logger,
		ServerAddr: r.ServerAddr,
		StartAlarmScheduler: func(ctx context.Context) error {
			return r.startBackgroundSchedulers(ctx)
		},
		StartHTTPServer:         r.StartHTTPServer,
		SetAlarmSchedulerCancel: r.setAlarmSchedulerCancel,
	})
}

func (r *AlarmWorkerRuntime) startBackgroundSchedulers(ctx context.Context) error {
	done := r.alarmSchedulerDone()
	if done != nil {
		defer close(done)
	}

	runners := []NamedScheduler{
		{Name: "scheduler", Scheduler: r.Scheduler},
		{Name: "notification-egress", Scheduler: r.NotificationEgress},
		{Name: "celebration", Scheduler: r.CelebrationRunner},
		{Name: "birthday-stream", Scheduler: r.BirthdayStreamRunner},
	}
	if r.ConfigSubscriber != nil {
		runners = append(runners, NamedScheduler{Name: "config-subscriber", Scheduler: configSubscriberRunner{r.ConfigSubscriber}})
	}

	return runNamedSchedulers(ctx, r.Logger, "alarm-worker-", runners)
}

func (r *AlarmWorkerRuntime) StartHTTPServer(errCh chan<- error) {
	if r == nil {
		return
	}

	r.HTTPServers.Start(r.Logger, errCh)
}
