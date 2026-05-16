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

package scraping

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

type RateLimiter struct {
	mu       sync.Mutex
	interval time.Duration
	lastTime time.Time
	seq      uint64

	distributed       distributedLimiter
	distributedLimit  int
	distributedWindow time.Duration
}

type localWaitReservation struct {
	prevLastTime time.Time
	seq          uint64
}

func NewRateLimiter(interval time.Duration) *RateLimiter {
	return &RateLimiter{interval: interval}
}

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

	waitTime, reservation, reserved, err := r.reserveLocalWait(ctx)
	if err != nil || !reserved {
		return err
	}
	return r.waitForLocalReservation(ctx, waitTime, reservation)
}

func (r *RateLimiter) reserveLocalWait(ctx context.Context) (time.Duration, localWaitReservation, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return 0, localWaitReservation{}, false, fmt.Errorf("rate limiter wait canceled: %w", err)
	}

	now := time.Now()
	if r.lastTime.IsZero() {
		r.commitLocalWait(now)
		return 0, localWaitReservation{}, false, nil
	}
	nextAllowed := r.lastTime.Add(r.interval)
	if now.After(nextAllowed) || now.Equal(nextAllowed) {
		r.commitLocalWait(now)
		return 0, localWaitReservation{}, false, nil
	}
	prevLastTime := r.lastTime
	r.commitLocalWait(nextAllowed)
	waitTime := nextAllowed.Sub(now)
	reservation := localWaitReservation{prevLastTime: prevLastTime, seq: r.seq}
	return waitTime, reservation, true, nil
}

func (r *RateLimiter) commitLocalWait(next time.Time) {
	r.lastTime = next
	r.seq++
}

func (r *RateLimiter) waitForLocalReservation(ctx context.Context, waitTime time.Duration, reservation localWaitReservation) error {
	timer := time.NewTimer(waitTime)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		r.rollbackLocalReservation(reservation)
		return fmt.Errorf("rate limiter wait canceled: %w", ctx.Err())
	case <-timer.C:
		return nil
	}
}

func (r *RateLimiter) rollbackLocalReservation(reservation localWaitReservation) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.seq == reservation.seq {
		r.lastTime = reservation.prevLastTime
		r.seq++
	}
}

func (r *RateLimiter) waitDistributed(ctx context.Context, bucket string) error {
	if r.distributed == nil {
		return nil
	}

	for {
		retryAfter, allowed, err := r.nextDistributedWait(ctx, bucket)
		if err != nil {
			return err
		}
		if allowed {
			return nil
		}
		if !ctxutil.SleepWithContext(ctx, retryAfter) {
			return fmt.Errorf("distributed rate limiter wait canceled: %w", ctx.Err())
		}
	}
}

func (r *RateLimiter) nextDistributedWait(ctx context.Context, bucket string) (time.Duration, bool, error) {
	decision, err := r.distributed.Allow(ctx, bucket, r.distributedLimit, r.distributedWindow)
	if err != nil {
		return 0, false, fmt.Errorf("distributed rate limiter allow failed: %w", err)
	}
	if decision.Allowed {
		return 0, true, nil
	}
	if decision.RetryAfter <= 0 {
		return 0, false, fmt.Errorf("distributed rate limiter denied without retry_after")
	}
	return decision.RetryAfter, false, nil
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
