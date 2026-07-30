package ratelimiter

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/service/ratelimit"
)

type distributedLimiterFunc func(context.Context, string, int, time.Duration) (ratelimit.Decision, error)

func (f distributedLimiterFunc) Allow(ctx context.Context, bucket string, limit int, window time.Duration) (ratelimit.Decision, error) {
	return f(ctx, bucket, limit, window)
}

func TestWaitWithBucketRollsBackImmediateLocalReservationOnDistributedError(t *testing.T) {
	limiter := New(time.Hour)
	if err := limiter.ConfigureDistributed(distributedLimiterFunc(func(context.Context, string, int, time.Duration) (ratelimit.Decision, error) {
		return ratelimit.Decision{}, errors.New("valkey down")
	}), 1, time.Second); err != nil {
		t.Fatalf("ConfigureDistributed() error = %v", err)
	}

	err := limiter.WaitWithBucket(context.Background(), "bucket")
	if err == nil {
		t.Fatal("WaitWithBucket() error = nil, want distributed error")
	}

	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	if !limiter.lastTime.IsZero() {
		t.Fatalf("lastTime was not rolled back: %v", limiter.lastTime)
	}
}

func TestWaitWithBucketRollsBackWaitedLocalReservationOnDistributedError(t *testing.T) {
	limiter := New(30 * time.Millisecond)

	if err := limiter.WaitWithBucket(context.Background(), "bucket"); err != nil {
		t.Fatalf("initial WaitWithBucket() error = %v", err)
	}
	limiter.mu.Lock()
	before := limiter.lastTime
	limiter.mu.Unlock()

	if err := limiter.ConfigureDistributed(distributedLimiterFunc(func(context.Context, string, int, time.Duration) (ratelimit.Decision, error) {
		return ratelimit.Decision{}, errors.New("valkey down")
	}), 1, time.Second); err != nil {
		t.Fatalf("ConfigureDistributed() error = %v", err)
	}

	err := limiter.WaitWithBucket(context.Background(), "bucket")
	if err == nil {
		t.Fatal("WaitWithBucket() error = nil, want distributed error")
	}

	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	if !limiter.lastTime.Equal(before) {
		t.Fatalf("lastTime advanced after distributed error: before=%v after=%v", before, limiter.lastTime)
	}
}
