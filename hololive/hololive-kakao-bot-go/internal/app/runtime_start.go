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
	"log/slog"
)

// Start: 봇의 모든 구성 요소(스케줄러, 알람 체커, 관리자 서버)를 시작합니다.
func (r *BotRuntime) Start(ctx context.Context, errCh chan<- error) {
	if r == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}

	r.startSchedulers(ctx, errCh)
	r.startBot(ctx)

	r.StartHTTPServer(errCh)
	if r.Logger != nil && r.ServerAddr != "" {
		r.Logger.Info("Bot HTTP server started", slog.String("addr", r.ServerAddr))
	}
}

func (r *BotRuntime) startSchedulers(ctx context.Context, errCh chan<- error) {
	r.startAlarmScheduler(ctx)

	if r.ConfigSubscriber != nil {
		go r.ConfigSubscriber.Run(ctx)
		r.logInfo("Config subscriber started")
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
	r.logInfo("Alarm runtime scheduler started")
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
