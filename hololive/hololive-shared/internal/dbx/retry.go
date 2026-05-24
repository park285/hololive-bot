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

package dbx

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/park285/shared-go/pkg/backoff"
)

// 스키마 마이그레이션이 완료되기 전 앱이 시작되는 Race Condition 방어용.
func OpenWithRetry(
	ctx context.Context,
	config Config,
	opt OpenOptions,
) (*Client, error) {
	retry := normalizeOpenRetryConfig(opt.Retry)
	logger := openRetryLogger(opt.Logger)

	var lastErr error
	for attempt := range retry.MaxAttempts {
		client, err := Open(ctx, config, opt)
		if err == nil {
			logOpenRetrySuccess(logger, attempt)
			return client, nil
		}

		lastErr = err
		if attempt >= retry.MaxAttempts-1 {
			break
		}

		delay := backoff.ComputeExponentialBackoff(attempt, retry.BaseDelay, retry.MaxDelay, 0)
		logOpenRetryAttempt(logger, retry, attempt, delay, err)
		if waitErr := waitOpenRetryDelay(ctx, delay); waitErr != nil {
			return nil, waitErr
		}
	}

	return nil, fmt.Errorf("postgres connect failed after %d attempts: %w", retry.MaxAttempts, lastErr)
}

func normalizeOpenRetryConfig(retry RetryConfig) RetryConfig {
	if retry.MaxAttempts <= 0 {
		retry.MaxAttempts = 5
	}
	if retry.BaseDelay <= 0 {
		retry.BaseDelay = 2 * time.Second
	}
	if retry.MaxDelay <= 0 {
		retry.MaxDelay = 30 * time.Second
	}
	return retry
}

func openRetryLogger(logger *slog.Logger) *slog.Logger {
	if logger == nil {
		return slog.Default()
	}
	return logger
}

func logOpenRetrySuccess(logger *slog.Logger, attempt int) {
	if attempt == 0 {
		return
	}
	logger.Info("postgres_connect_success_after_retry",
		slog.Int("attempts", attempt+1),
	)
}

func logOpenRetryAttempt(logger *slog.Logger, retry RetryConfig, attempt int, delay time.Duration, err error) {
	logger.Warn("postgres_connect_retry",
		slog.Int("attempt", attempt+1),
		slog.Int("max_attempts", retry.MaxAttempts),
		slog.Duration("delay", delay),
		slog.Any("error", err),
	)
}

func waitOpenRetryDelay(ctx context.Context, delay time.Duration) error {
	select {
	case <-ctx.Done():
		return fmt.Errorf("postgres connect canceled: %w", ctx.Err())
	case <-time.After(delay):
		return nil
	}
}
