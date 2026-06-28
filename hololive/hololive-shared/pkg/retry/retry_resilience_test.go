package retry

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestWithRetry_MaxDelayCapsComputedDelay(t *testing.T) {
	targetErr := errors.New("transient")
	var slept []time.Duration

	err := WithRetry(context.Background(), RetryOptions{
		MaxAttempts: 3,
		BaseDelay:   time.Second,
		MaxDelay:    1500 * time.Millisecond,
		Sleep: func(_ context.Context, d time.Duration) bool {
			slept = append(slept, d)
			return true
		},
	}, func(_ context.Context) error {
		return targetErr
	})

	if !errors.Is(err, targetErr) {
		t.Fatalf("expected target error, got %v", err)
	}
	if len(slept) != 2 {
		t.Fatalf("expected 2 sleeps, got %d", len(slept))
	}
	if slept[0] != time.Second {
		t.Fatalf("first delay = %v, want 1s", slept[0])
	}
	if slept[1] != 1500*time.Millisecond {
		t.Fatalf("second delay = %v, want capped 1.5s", slept[1])
	}
}

func TestWithRetry_DelayOverrideWinsBeforeMaxDelay(t *testing.T) {
	targetErr := errors.New("retry-after")
	var slept time.Duration

	err := WithRetry(context.Background(), RetryOptions{
		MaxAttempts: 2,
		BaseDelay:   time.Second,
		MaxDelay:    3 * time.Second,
		DelayOverride: func(err error, _ time.Duration) (time.Duration, bool) {
			if errors.Is(err, targetErr) {
				return 10 * time.Second, true
			}
			return 0, false
		},
		Sleep: func(_ context.Context, d time.Duration) bool {
			slept = d
			return true
		},
	}, func(_ context.Context) error {
		return targetErr
	})

	if !errors.Is(err, targetErr) {
		t.Fatalf("expected target error, got %v", err)
	}
	if slept != 3*time.Second {
		t.Fatalf("delay = %v, want max-delay capped 3s", slept)
	}
}
