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
	crand "crypto/rand"
	"fmt"
	"math"
	"math/big"
	"time"

	"github.com/kapu/hololive-shared/internal/ctxutil"
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

// attempt는 0부터 시작합니다 (첫 번째 재시도 = attempt 0).
// 계산식: base * 2^attempt + random(0, jitter)
func ComputeBackoffDelay(attempt int, base, jitter time.Duration) time.Duration {
	delay := base * time.Duration(math.Pow(2, float64(attempt)))
	if jitter > 0 {
		delay += randomJitter(jitter)
	}
	return delay
}

func randomJitter(maxDuration time.Duration) time.Duration {
	if maxDuration <= 0 {
		return 0
	}
	n, err := crand.Int(crand.Reader, big.NewInt(int64(maxDuration)))
	if err != nil {
		return 0
	}
	return time.Duration(n.Int64())
}

// fn이 nil 에러를 반환하면 즉시 성공으로 종료됩니다.
// 모든 재시도가 실패하면 마지막 에러를 반환합니다.
func WithRetry(ctx context.Context, opts RetryOptions, fn func(ctx context.Context) error) error {
	if opts.MaxAttempts < 1 {
		opts.MaxAttempts = 1
	}
	if opts.Sleep == nil {
		opts.Sleep = ctxutil.SleepWithContext
	}

	var lastErr error

	for attempt := range opts.MaxAttempts {
		if ctx.Err() != nil {
			if lastErr != nil {
				return lastErr
			}
			return fmt.Errorf("context error: %w", ctx.Err())
		}

		err := fn(ctx)
		if err == nil {
			return nil
		}

		lastErr = err

		if opts.ShouldRetry != nil && !opts.ShouldRetry(err) {
			return fmt.Errorf("retry aborted by ShouldRetry predicate: %w", err)
		}

		if attempt >= opts.MaxAttempts-1 {
			break
		}

		delay := ComputeBackoffDelay(attempt, opts.BaseDelay, opts.Jitter)

		if opts.OnRetry != nil {
			opts.OnRetry(attempt+1, err, delay)
		}

		if !opts.Sleep(ctx, delay) {
			return lastErr
		}
	}

	return lastErr
}

func DefaultRetryOptions(maxAttempts int, baseDelay, jitter time.Duration) RetryOptions {
	return RetryOptions{
		MaxAttempts: maxAttempts,
		BaseDelay:   baseDelay,
		Jitter:      jitter,
	}
}
