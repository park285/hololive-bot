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
	"log/slog"
	"time"

	"github.com/kapu/hololive-shared/pkg/service/alarm/queue"
	"github.com/kapu/hololive-shared/pkg/util"
)

func (r *Runtime) waitForPGDispatchSignal(ctx context.Context) bool {
	if r == nil || r.cfg == nil {
		return sleepContext(ctx, time.Second)
	}
	if !r.cfg.Dispatch.WakeupEnabled || r.cacheSvc == nil {
		return sleepContext(ctx, r.cfg.Dispatch.PollInterval)
	}
	timeout := r.cfg.Dispatch.PollInterval
	if timeout <= 0 {
		timeout = time.Second
	}
	cmd := r.cacheSvc.B().Brpop().Key(queue.AlarmDispatchWakeupQueue).Timeout(timeout.Seconds()).Build()
	results := r.cacheSvc.DoMulti(ctx, cmd)
	if len(results) != 1 {
		r.logger.Warn("Dispatch wakeup wait returned unexpected result count", slog.Int("result_count", len(results)))
		return sleepContext(ctx, timeout)
	}
	if _, err := results[0].AsStrSlice(); err != nil && !util.IsValkeyNil(err) {
		if ctx.Err() != nil {
			return false
		}
		r.logger.Warn("Dispatch wakeup wait failed", slog.Any("error", err))
		return sleepContext(ctx, timeout)
	}
	return ctx.Err() == nil
}

func sleepContext(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		d = time.Second
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
