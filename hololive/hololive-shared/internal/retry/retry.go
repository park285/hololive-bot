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

// Package retry는 지수 백오프 + 지터를 적용한 재시도 유틸리티를 제공합니다.
package retry

import (
	"context"
	"fmt"
	"time"

	"github.com/kapu/hololive-shared/internal/ctxutil"
	"github.com/park285/shared-go/pkg/backoff"
)

type RetryOptions struct {
	// MaxAttempts: 최대 재시도 횟수 (1 이상)
	MaxAttempts int
	// BaseDelay: 첫 번째 재시도 전 기본 대기 시간
	BaseDelay time.Duration
	// Jitter: 대기 시간에 추가될 랜덤 지터 (Thundering Herd 방지)
	Jitter time.Duration
	// ShouldRetry: 에러 발생 시 재시도 여부를 결정하는 함수 (nil이면 항상 재시도)
	ShouldRetry func(err error) bool
	// OnRetry: 재시도 시 호출되는 콜백 (로깅용, optional)
	OnRetry func(attempt int, err error, delay time.Duration)
	// Sleep: 대기 함수 (테스트용 주입 가능, nil이면 ctxutil.SleepWithContext 사용)
	Sleep func(ctx context.Context, d time.Duration) bool
}

func ComputeBackoffDelay(attempt int, base, jitter time.Duration) time.Duration {
	return backoff.ComputeExponentialBackoff(attempt, base, 0, jitter)
}

// fn이 nil 에러를 반환하면 즉시 성공으로 종료됩니다.
// 모든 재시도가 실패하면 마지막 에러를 반환합니다.
func WithRetry(ctx context.Context, opts RetryOptions, fn func(ctx context.Context) error) error {
	opts = normalizeRetryOptions(opts)

	var lastErr error

	for attempt := range opts.MaxAttempts {
		outcome := runRetryAttempt(ctx, opts, fn, attempt, lastErr)
		if outcome.done {
			return outcome.err
		}
		lastErr = outcome.lastErr
	}

	return lastErr
}

type retryAttemptOutcome struct {
	lastErr error
	done    bool
	err     error
}

func runRetryAttempt(
	ctx context.Context,
	opts RetryOptions,
	fn func(ctx context.Context) error,
	attempt int,
	lastErr error,
) retryAttemptOutcome {
	if err := retryContextError(ctx, lastErr); err != nil {
		return retryAttemptOutcome{done: true, err: err}
	}

	err := fn(ctx)
	if err == nil {
		return retryAttemptOutcome{done: true}
	}

	return handleRetryFailure(ctx, opts, attempt, err)
}

func handleRetryFailure(ctx context.Context, opts RetryOptions, attempt int, err error) retryAttemptOutcome {
	if !shouldContinueRetry(opts, err) {
		return retryAttemptOutcome{done: true, err: fmt.Errorf("retry aborted by ShouldRetry predicate: %w", err)}
	}
	if attempt >= opts.MaxAttempts-1 {
		return retryAttemptOutcome{done: true, err: err}
	}
	if !sleepBeforeRetry(ctx, opts, attempt, err) {
		return retryAttemptOutcome{done: true, err: err}
	}
	return retryAttemptOutcome{lastErr: err}
}

func normalizeRetryOptions(opts RetryOptions) RetryOptions {
	if opts.MaxAttempts < 1 {
		opts.MaxAttempts = 1
	}
	if opts.Sleep == nil {
		opts.Sleep = ctxutil.SleepWithContext
	}
	return opts
}

func retryContextError(ctx context.Context, lastErr error) error {
	if ctx.Err() == nil {
		return nil
	}
	if lastErr != nil {
		return lastErr
	}
	return fmt.Errorf("context error: %w", ctx.Err())
}

func shouldContinueRetry(opts RetryOptions, err error) bool {
	return opts.ShouldRetry == nil || opts.ShouldRetry(err)
}

func sleepBeforeRetry(ctx context.Context, opts RetryOptions, attempt int, err error) bool {
	delay := ComputeBackoffDelay(attempt, opts.BaseDelay, opts.Jitter)
	if opts.OnRetry != nil {
		opts.OnRetry(attempt+1, err, delay)
	}
	return opts.Sleep(ctx, delay)
}

func DefaultRetryOptions(maxAttempts int, baseDelay, jitter time.Duration) RetryOptions {
	return RetryOptions{
		MaxAttempts: maxAttempts,
		BaseDelay:   baseDelay,
		Jitter:      jitter,
	}
}
