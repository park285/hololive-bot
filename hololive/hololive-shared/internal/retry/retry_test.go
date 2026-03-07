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

package retry

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestComputeBackoffDelay_ExponentialGrowth(t *testing.T) {
	base := 100 * time.Millisecond

	tests := []struct {
		attempt  int
		expected time.Duration
	}{
		{0, 100 * time.Millisecond},
		{1, 200 * time.Millisecond},
		{2, 400 * time.Millisecond},
		{3, 800 * time.Millisecond},
	}

	for _, tt := range tests {
		got := ComputeBackoffDelay(tt.attempt, base, 0)
		if got != tt.expected {
			t.Errorf("ComputeBackoffDelay(%d, %v, 0) = %v, want %v",
				tt.attempt, base, got, tt.expected)
		}
	}
}

func TestComputeBackoffDelay_WithJitter(t *testing.T) {
	base := 100 * time.Millisecond
	jitter := 50 * time.Millisecond

	for range 100 {
		delay := ComputeBackoffDelay(0, base, jitter)
		if delay < base || delay >= base+jitter {
			t.Errorf("delay %v outside expected range [%v, %v)", delay, base, base+jitter)
		}
	}
}

func TestWithRetry_SuccessOnFirstAttempt(t *testing.T) {
	callCount := 0
	fakeSleep := func(_ context.Context, _ time.Duration) bool {
		t.Error("sleep should not be called on first success")
		return true
	}

	err := WithRetry(context.Background(), RetryOptions{
		MaxAttempts: 3,
		BaseDelay:   time.Second,
		Sleep:       fakeSleep,
	}, func(_ context.Context) error {
		callCount++
		return nil
	})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if callCount != 1 {
		t.Errorf("expected 1 call, got %d", callCount)
	}
}

func TestWithRetry_SuccessAfterRetries(t *testing.T) {
	callCount := 0
	sleepCount := 0
	targetErr := errors.New("transient error")

	err := WithRetry(context.Background(), RetryOptions{
		MaxAttempts: 3,
		BaseDelay:   time.Millisecond,
		Sleep: func(_ context.Context, _ time.Duration) bool {
			sleepCount++
			return true
		},
	}, func(_ context.Context) error {
		callCount++
		if callCount < 3 {
			return targetErr
		}
		return nil
	})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if callCount != 3 {
		t.Errorf("expected 3 calls, got %d", callCount)
	}
	if sleepCount != 2 {
		t.Errorf("expected 2 sleeps, got %d", sleepCount)
	}
}

func TestWithRetry_AllAttemptsFail(t *testing.T) {
	callCount := 0
	targetErr := errors.New("persistent error")

	err := WithRetry(context.Background(), RetryOptions{
		MaxAttempts: 3,
		BaseDelay:   time.Millisecond,
		Sleep: func(_ context.Context, _ time.Duration) bool {
			return true
		},
	}, func(_ context.Context) error {
		callCount++
		return targetErr
	})

	if !errors.Is(err, targetErr) {
		t.Errorf("expected targetErr, got %v", err)
	}
	if callCount != 3 {
		t.Errorf("expected 3 calls, got %d", callCount)
	}
}

func TestWithRetry_ShouldRetryFalse(t *testing.T) {
	callCount := 0
	permanentErr := errors.New("permanent error")

	err := WithRetry(context.Background(), RetryOptions{
		MaxAttempts: 3,
		BaseDelay:   time.Millisecond,
		ShouldRetry: func(_ error) bool {
			return false
		},
		Sleep: func(_ context.Context, _ time.Duration) bool {
			t.Error("sleep should not be called when ShouldRetry returns false")
			return true
		},
	}, func(_ context.Context) error {
		callCount++
		return permanentErr
	})

	if !errors.Is(err, permanentErr) {
		t.Errorf("expected permanentErr, got %v", err)
	}
	if callCount != 1 {
		t.Errorf("expected 1 call, got %d", callCount)
	}
}

func TestWithRetry_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	callCount := 0

	err := WithRetry(ctx, RetryOptions{
		MaxAttempts: 5,
		BaseDelay:   time.Millisecond,
		Sleep: func(_ context.Context, _ time.Duration) bool {
			if callCount >= 2 {
				cancel()
				return false
			}
			return true
		},
	}, func(_ context.Context) error {
		callCount++
		return errors.New("error")
	})

	if err == nil {
		t.Error("expected error after context cancellation")
	}
	if callCount > 3 {
		t.Errorf("too many calls after cancellation: %d", callCount)
	}
}

func TestWithRetry_OnRetryCallback(t *testing.T) {
	retryAttempts := []int{}

	_ = WithRetry(context.Background(), RetryOptions{
		MaxAttempts: 3,
		BaseDelay:   time.Millisecond,
		OnRetry: func(attempt int, _ error, _ time.Duration) {
			retryAttempts = append(retryAttempts, attempt)
		},
		Sleep: func(_ context.Context, _ time.Duration) bool {
			return true
		},
	}, func(_ context.Context) error {
		return errors.New("error")
	})

	if len(retryAttempts) != 2 {
		t.Errorf("expected 2 OnRetry calls, got %d", len(retryAttempts))
	}
	if retryAttempts[0] != 1 || retryAttempts[1] != 2 {
		t.Errorf("unexpected retry attempts: %v", retryAttempts)
	}
}

func TestDefaultRetryOptions(t *testing.T) {
	opts := DefaultRetryOptions(5, 100*time.Millisecond, 50*time.Millisecond)

	if opts.MaxAttempts != 5 {
		t.Errorf("expected MaxAttempts=5, got %d", opts.MaxAttempts)
	}
	if opts.BaseDelay != 100*time.Millisecond {
		t.Errorf("expected BaseDelay=100ms, got %v", opts.BaseDelay)
	}
	if opts.Jitter != 50*time.Millisecond {
		t.Errorf("expected Jitter=50ms, got %v", opts.Jitter)
	}
}
