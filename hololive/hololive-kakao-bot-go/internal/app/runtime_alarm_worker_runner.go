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

package app

import (
	"context"

	appruntime "github.com/kapu/hololive-kakao-bot-go/internal/app/runtime"
)

func (r *AlarmWorkerRuntime) Run() {
	if r == nil {
		return
	}

	appruntime.Run(r.Logger, r.Start, r.Shutdown)
}

func (r *AlarmWorkerRuntime) Start(ctx context.Context, errCh chan<- error) {
	if r == nil {
		return
	}

	appruntime.Start(ctx, errCh, appruntime.StartHooks{
		Logger:     r.Logger,
		ServerAddr: r.ServerAddr,
		StartAlarmScheduler: func(ctx context.Context) {
			if r.Scheduler != nil {
				r.Scheduler.Start(ctx)
			}
		},
		RunConfigSubscriber: func(ctx context.Context) {
			if r.ConfigSubscriber != nil {
				r.ConfigSubscriber.Run(ctx)
			}
		},
		StartHTTPServer:         r.StartHTTPServer,
		SetAlarmSchedulerCancel: r.setAlarmSchedulerCancel,
	})
}

func (r *AlarmWorkerRuntime) StartHTTPServer(errCh chan<- error) {
	if r == nil {
		return
	}

	appruntime.StartHTTPServer(r.HttpServer, r.Logger, errCh)
}
