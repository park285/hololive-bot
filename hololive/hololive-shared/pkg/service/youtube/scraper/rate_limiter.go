package scraper

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/kapu/hololive-shared/internal/ctxutil"
	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/service/ratelimit"
)

type distributedLimiter interface {
	Allow(ctx context.Context, bucket string, limit int, window time.Duration) (ratelimit.Decision, error)
}

// RateLimiter: 간격 기반 레이트 리미터 (slot 예약 패턴, 취소 시 rollback)
type RateLimiter struct {
	mu       sync.Mutex
	interval time.Duration
	lastTime time.Time
	seq      uint64

	distributed       distributedLimiter
	distributedLimit  int
	distributedWindow time.Duration
}

func NewRateLimiter(interval time.Duration) *RateLimiter {
	return &RateLimiter{interval: interval}
}

// ConfigureDistributed: 분산 레이트 리미터를 설정합니다.
func (r *RateLimiter) ConfigureDistributed(limiter distributedLimiter, limit int, window time.Duration) error {
	if limiter == nil {
		return fmt.Errorf("configure distributed limiter: limiter must not be nil")
	}
	if limit <= 0 {
		return fmt.Errorf("configure distributed limiter: limit must be greater than zero")
	}
	if window <= 0 {
		return fmt.Errorf("configure distributed limiter: window must be greater than zero")
	}
	r.distributed = limiter
	r.distributedLimit = limit
	r.distributedWindow = window
	return nil
}

func (r *RateLimiter) Wait(ctx context.Context) error {
	return r.WaitWithBucket(ctx, "default")
}

// WaitWithBucket: 로컬 + 분산 레이트 리미터를 함께 적용합니다.
func (r *RateLimiter) WaitWithBucket(ctx context.Context, bucket string) error {
	if bucket == "" {
		bucket = "default"
	}
	if err := r.waitLocal(ctx); err != nil {
		return err
	}
	return r.waitDistributed(ctx, bucket)
}

func (r *RateLimiter) waitLocal(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("rate limiter wait canceled: %w", err)
	}

	r.mu.Lock()
	if err := ctx.Err(); err != nil {
		r.mu.Unlock()
		return fmt.Errorf("rate limiter wait canceled: %w", err)
	}

	now := time.Now()
	if r.lastTime.IsZero() {
		r.lastTime = now
		r.seq++
		r.mu.Unlock()
		return nil
	}
	nextAllowed := r.lastTime.Add(r.interval)
	if now.After(nextAllowed) || now.Equal(nextAllowed) {
		r.lastTime = now
		r.seq++
		r.mu.Unlock()
		return nil
	}
	prevLastTime := r.lastTime
	r.lastTime = nextAllowed
	r.seq++
	reservedSeq := r.seq
	waitTime := nextAllowed.Sub(now)
	r.mu.Unlock()

	timer := time.NewTimer(waitTime)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		r.mu.Lock()
		if r.seq == reservedSeq {
			r.lastTime = prevLastTime
			r.seq++
		}
		r.mu.Unlock()
		return fmt.Errorf("rate limiter wait canceled: %w", ctx.Err())
	case <-timer.C:
		return nil
	}
}

func (r *RateLimiter) waitDistributed(ctx context.Context, bucket string) error {
	if r.distributed == nil {
		return nil
	}

	for {
		decision, err := r.distributed.Allow(ctx, bucket, r.distributedLimit, r.distributedWindow)
		if err != nil {
			return fmt.Errorf("distributed rate limiter allow failed: %w", err)
		}
		if decision.Allowed {
			return nil
		}
		if decision.RetryAfter <= 0 {
			return fmt.Errorf("distributed rate limiter denied without retry_after")
		}
		if !ctxutil.SleepWithContext(ctx, decision.RetryAfter) {
			return fmt.Errorf("distributed rate limiter wait canceled: %w", ctx.Err())
		}
	}
}

func distributedBucketFromURL(pageURL string) string {
	base := constants.YouTubeScraperDistributedRateLimitConfig.BucketBase
	parsed, err := url.Parse(pageURL)
	if err != nil {
		return base + ":unknown"
	}
	path := strings.Trim(parsed.Path, "/")
	if path == "" {
		path = "root"
	}
	path = strings.ReplaceAll(path, "/", ":")
	return base + ":" + path
}
