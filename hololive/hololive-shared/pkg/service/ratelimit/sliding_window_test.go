package ratelimit

import (
	"context"
	"io"
	"log/slog"
	"net"
	"strconv"
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

	second, err := limiter.Allow(ctx, "youtube:channels", 2, window)
	if err != nil {
		t.Fatalf("second allow: %v", err)
	}
	if !second.Allowed {
		t.Fatalf("second call should be allowed")
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := limiter.Allow(ctx, tt.bucket, tt.limit, tt.window); err == nil {
				t.Fatalf("expected error but got nil")
			}
		})
	}
}
