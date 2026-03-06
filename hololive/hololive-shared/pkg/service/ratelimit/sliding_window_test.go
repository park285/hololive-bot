package ratelimit

import (
	"context"
	"io"
	"log/slog"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"

	"github.com/kapu/hololive-shared/pkg/service/cache"
)

func newTestLimiter(t *testing.T) *SlidingWindowLimiter {
	t.Helper()

	mini := miniredis.RunT(t)
	host, portStr, err := net.SplitHostPort(mini.Addr())
	if err != nil {
		t.Fatalf("split host port: %v", err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("parse port: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cacheSvc, err := cache.NewCacheService(context.Background(), cache.Config{
		Host:         host,
		Port:         port,
		DB:           0,
		DisableCache: true,
	}, logger)
	if err != nil {
		t.Fatalf("new cache service: %v", err)
	}

	limiter, err := NewSlidingWindowLimiter(cacheSvc, "test:ratelimit", logger)
	if err != nil {
		t.Fatalf("new sliding window limiter: %v", err)
	}

	t.Cleanup(func() {
		_ = cacheSvc.Close()
		mini.Close()
	})

	return limiter
}

func TestAllowEnforcesLimit(t *testing.T) {
	limiter := newTestLimiter(t)
	ctx := context.Background()

	window := 120 * time.Millisecond

	first, err := limiter.Allow(ctx, "youtube:channels", 2, window)
	if err != nil {
		t.Fatalf("first allow: %v", err)
	}
	if !first.Allowed {
		t.Fatalf("first call should be allowed")
	}
	if first.Current != 1 || first.Remaining != 1 {
		t.Fatalf("unexpected first decision: %+v", first)
	}

	second, err := limiter.Allow(ctx, "youtube:channels", 2, window)
	if err != nil {
		t.Fatalf("second allow: %v", err)
	}
	if !second.Allowed {
		t.Fatalf("second call should be allowed")
	}
	if second.Current != 2 || second.Remaining != 0 {
		t.Fatalf("unexpected second decision: %+v", second)
	}

	third, err := limiter.Allow(ctx, "youtube:channels", 2, window)
	if err != nil {
		t.Fatalf("third allow: %v", err)
	}
	if third.Allowed {
		t.Fatalf("third call should be denied")
	}
	if third.RetryAfter <= 0 {
		t.Fatalf("retry after should be positive: %v", third.RetryAfter)
	}
	if third.Current != 2 || third.Remaining != 0 {
		t.Fatalf("unexpected third decision: %+v", third)
	}
}

func TestAllowAfterWindowExpires(t *testing.T) {
	limiter := newTestLimiter(t)
	ctx := context.Background()

	window := 60 * time.Millisecond

	first, err := limiter.Allow(ctx, "youtube:videos", 1, window)
	if err != nil {
		t.Fatalf("first allow: %v", err)
	}
	if !first.Allowed {
		t.Fatalf("first call should be allowed")
	}

	second, err := limiter.Allow(ctx, "youtube:videos", 1, window)
	if err != nil {
		t.Fatalf("second allow: %v", err)
	}
	if second.Allowed {
		t.Fatalf("second call should be denied")
	}

	time.Sleep(90 * time.Millisecond)

	third, err := limiter.Allow(ctx, "youtube:videos", 1, window)
	if err != nil {
		t.Fatalf("third allow: %v", err)
	}
	if !third.Allowed {
		t.Fatalf("third call should be allowed after window")
	}
	if third.Current != 1 || third.Remaining != 0 {
		t.Fatalf("unexpected third decision after expiry: %+v", third)
	}
}

func TestAllowBucketIsolation(t *testing.T) {
	limiter := newTestLimiter(t)
	ctx := context.Background()

	window := 100 * time.Millisecond

	firstA, err := limiter.Allow(ctx, "bucket:A", 1, window)
	if err != nil {
		t.Fatalf("bucket A first allow: %v", err)
	}
	if !firstA.Allowed {
		t.Fatalf("bucket A first call should be allowed")
	}

	secondA, err := limiter.Allow(ctx, "bucket:A", 1, window)
	if err != nil {
		t.Fatalf("bucket A second allow: %v", err)
	}
	if secondA.Allowed {
		t.Fatalf("bucket A second call should be denied")
	}

	firstB, err := limiter.Allow(ctx, "bucket:B", 1, window)
	if err != nil {
		t.Fatalf("bucket B first allow: %v", err)
	}
	if !firstB.Allowed {
		t.Fatalf("bucket B first call should be allowed (isolation)")
	}
}

func TestAllowValidation(t *testing.T) {
	limiter := newTestLimiter(t)
	ctx := context.Background()

	tests := []struct {
		name   string
		bucket string
		limit  int
		window time.Duration
	}{
		{
			name:   "empty bucket",
			bucket: "",
			limit:  1,
			window: time.Second,
		},
		{
			name:   "invalid limit",
			bucket: "bucket",
			limit:  0,
			window: time.Second,
		},
		{
			name:   "invalid window",
			bucket: "bucket",
			limit:  1,
			window: 0,
		},
		{
			name:   "sub millisecond window",
			bucket: "bucket",
			limit:  1,
			window: 500 * time.Microsecond,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := limiter.Allow(ctx, tt.bucket, tt.limit, tt.window); err == nil {
				t.Fatalf("expected error but got nil")
			}
		})
	}
}

func TestAllowConcurrentLimit(t *testing.T) {
	limiter := newTestLimiter(t)
	ctx := context.Background()

	const (
		limit       = 3
		concurrency = 10
	)

	window := 250 * time.Millisecond

	var allowedCount atomic.Int64
	var deniedCount atomic.Int64
	results := make(chan Decision, concurrency)

	var wg sync.WaitGroup
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			decision, err := limiter.Allow(ctx, "youtube:concurrent", limit, window)
			if err != nil {
				t.Errorf("allow concurrently: %v", err)
				return
			}
			if decision.Allowed {
				allowedCount.Add(1)
			} else {
				deniedCount.Add(1)
			}
			results <- decision
		}()
	}

	wg.Wait()
	close(results)

	if got := allowedCount.Load(); got != limit {
		t.Fatalf("allowed count = %d, want %d", got, limit)
	}
	if got := deniedCount.Load(); got != concurrency-limit {
		t.Fatalf("denied count = %d, want %d", got, concurrency-limit)
	}

	for decision := range results {
		if decision.Current <= 0 || decision.Current > limit {
			t.Fatalf("unexpected current count: %+v", decision)
		}
		if decision.Remaining < 0 || decision.Remaining > limit {
			t.Fatalf("unexpected remaining count: %+v", decision)
		}
		if !decision.Allowed && decision.RetryAfter <= 0 {
			t.Fatalf("denied decision should include retry_after: %+v", decision)
		}
	}
}
