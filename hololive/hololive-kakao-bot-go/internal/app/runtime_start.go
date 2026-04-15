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
	"errors"

	appruntime "github.com/kapu/hololive-kakao-bot-go/internal/app/runtime"
)

func (r *BotRuntime) Start(ctx context.Context, errCh chan<- error) {
	if r == nil {
		return
	}

	appruntime.Start(ctx, errCh, appruntime.StartHooks{
		Logger:     r.Logger,
		ServerAddr: r.ServerAddr,
		StartAlarmScheduler: func(ctx context.Context) {
			if r.AlarmScheduler != nil {
				r.AlarmScheduler.Start(ctx)
			}
		},
		RunConfigSubscriber: func(ctx context.Context) {
			if r.ConfigSubscriber != nil {
				r.ConfigSubscriber.Run(ctx)
			}
		},
		StartBot: func(ctx context.Context) error {
			if r.Bot == nil {
				return nil
			}
			return r.Bot.Start(ctx)
		},
		StartHTTPServer:         r.StartHTTPServer,
		SetAlarmSchedulerCancel: r.setAlarmSchedulerCancel,
	})
}

func (r *BotRuntime) setAlarmSchedulerCancel(cancel context.CancelFunc) {
	if cancel == nil {
		return
	}

	r.alarmSchedulerMu.Lock()
	prevCancel := r.alarmSchedulerCancel

	r.alarmSchedulerCancel = cancel
	r.alarmSchedulerMu.Unlock()

	if prevCancel != nil {
		prevCancel()
	}
}

func (r *BotRuntime) clearAlarmSchedulerCancel() bool {
	r.alarmSchedulerMu.Lock()
	cancel := r.alarmSchedulerCancel

	r.alarmSchedulerCancel = nil
	r.alarmSchedulerMu.Unlock()

	if cancel != nil {
		cancel()
		return true
	}

	return false
}

func (r *BotRuntime) startSchedulers(ctx context.Context, errCh chan<- error) {
	r.startAlarmScheduler(ctx)

	if r.ConfigSubscriber != nil {
		go r.ConfigSubscriber.Run(ctx)
		r.logInfo("Config subscriber started")
	}

	if r.HttpServer != nil {
		r.StartHTTPServer(errCh)
	}
}

func (r *BotRuntime) startAlarmScheduler(ctx context.Context) {
	if r.AlarmScheduler == nil {
		r.logInfo("Alarm runtime scheduler not configured")
		return
	}

	if ctx == nil {
		ctx = context.Background()
	}

	alarmCtx, alarmCancel := context.WithCancel(ctx)
	r.setAlarmSchedulerCancel(alarmCancel)

	go r.AlarmScheduler.Start(alarmCtx)
	if r.Logger != nil {
		r.Logger.Info("Alarm runtime scheduler started")
	}
}

func (r *BotRuntime) startBot(ctx context.Context) {
	if r.Bot == nil {
		return
	}

	go func() {
		if err := r.Bot.Start(ctx); err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				r.logInfo("Bot alarm checker stopped (context done)")
			} else {
				r.logError("Bot alarm checker error", err)
			}
		}
	}()
}
