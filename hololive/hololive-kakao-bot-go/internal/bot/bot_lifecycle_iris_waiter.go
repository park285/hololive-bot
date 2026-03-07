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
	if l == nil || l.irisClient == nil {
		return fmt.Errorf("wait for iris ready: iris client is not configured")
	}

	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(retryInterval)
	defer ticker.Stop()

	attempt := 0
	startedAt := time.Now()
	lastWarnLoggedAt := time.Time{}
	for {
		attempt++
		pingCtx, pingCancel := context.WithTimeout(waitCtx, pingTimeout)
		ready := l.irisClient.Ping(pingCtx)
		pingCancel()

		if ready {
			if attempt > 1 {
				l.logInfo(
					"Iris server became ready after retry",
					slog.Int("attempt", attempt),
					slog.Duration("elapsed", time.Since(startedAt)),
				)
			}
			return nil
		}

		now := time.Now()
		if attempt == 1 || lastWarnLoggedAt.IsZero() || now.Sub(lastWarnLoggedAt) >= time.Minute {
			l.logWarn(
				"Iris server not ready, retrying",
				slog.Int("attempt", attempt),
				slog.Duration("retry_interval", retryInterval),
				slog.Duration("elapsed", now.Sub(startedAt)),
			)
			lastWarnLoggedAt = now
		}

		select {
		case <-waitCtx.Done():
			if errors.Is(waitCtx.Err(), context.DeadlineExceeded) {
				return fmt.Errorf("wait for iris ready: timeout after %s", timeout)
			}
			return fmt.Errorf("wait for iris ready: canceled: %w", waitCtx.Err())
		case <-ticker.C:
		}
	}
}
