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

package bot

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

func (l *BotLifecycle) WaitUntilIrisReady(
	ctx context.Context,
	timeout, retryInterval, pingTimeout time.Duration,
) error {
	if err := l.validateIrisReadyWaiter(); err != nil {
		return err
	}

	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(retryInterval)
	defer ticker.Stop()

	return l.runIrisReadyWaitLoop(waitCtx, ticker.C, timeout, retryInterval, pingTimeout)
}

func (l *BotLifecycle) validateIrisReadyWaiter() error {
	if l == nil || l.irisClient == nil {
		return errors.New("wait for iris ready: iris client is not configured")
	}
	return nil
}

func (l *BotLifecycle) runIrisReadyWaitLoop(
	waitCtx context.Context,
	tick <-chan time.Time,
	timeout, retryInterval, pingTimeout time.Duration,
) error {
	attempt := 0
	startedAt := time.Now()
	lastWarnLoggedAt := time.Time{}

	for {
		attempt++

		if l.pingIrisReady(waitCtx, pingTimeout) {
			l.logIrisReadyAfterRetry(attempt, startedAt)
			return nil
		}

		if loggedAt, ok := l.logIrisNotReadyRetry(attempt, retryInterval, startedAt, lastWarnLoggedAt); ok {
			lastWarnLoggedAt = loggedAt
		}

		if err := waitNextIrisReadyRetry(waitCtx, tick, timeout); err != nil {
			return err
		}
	}
}

func (l *BotLifecycle) pingIrisReady(ctx context.Context, pingTimeout time.Duration) bool {
	pingCtx, pingCancel := context.WithTimeout(ctx, pingTimeout)
	defer pingCancel()
	return l.irisClient.Ping(pingCtx)
}

func (l *BotLifecycle) logIrisReadyAfterRetry(attempt int, startedAt time.Time) {
	if attempt <= 1 {
		return
	}
	l.logInfo(
		"Iris server became ready after retry",
		slog.Int("attempt", attempt),
		slog.Duration("elapsed", time.Since(startedAt)),
	)
}

func (l *BotLifecycle) logIrisNotReadyRetry(attempt int, retryInterval time.Duration, startedAt time.Time, lastWarnLoggedAt time.Time) (time.Time, bool) {
	now := time.Now()
	if attempt != 1 && !lastWarnLoggedAt.IsZero() && now.Sub(lastWarnLoggedAt) < time.Minute {
		return lastWarnLoggedAt, false
	}
	l.logWarn(
		"Iris server not ready, retrying",
		slog.Int("attempt", attempt),
		slog.Duration("retry_interval", retryInterval),
		slog.Duration("elapsed", now.Sub(startedAt)),
	)
	return now, true
}

func waitNextIrisReadyRetry(ctx context.Context, tick <-chan time.Time, timeout time.Duration) error {
	select {
	case <-ctx.Done():
		return irisReadyWaitErr(ctx.Err(), timeout)
	case <-tick:
		return nil
	}
}

func irisReadyWaitErr(err error, timeout time.Duration) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("wait for iris ready: timeout after %s", timeout)
	}
	return fmt.Errorf("wait for iris ready: canceled: %w", err)
}
