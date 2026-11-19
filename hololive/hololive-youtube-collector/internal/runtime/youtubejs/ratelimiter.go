package youtubejs

import (
	"context"
	"fmt"
	"sync"
	"time"
)

type RateLimiter struct {
	mu       sync.Mutex
	interval time.Duration
	last     time.Time
	seq      uint64
}

type rateLimiterReservation struct {
	previous time.Time
	seq      uint64
}

func NewRateLimiter(interval time.Duration) *RateLimiter {
	return &RateLimiter{interval: interval}
}

func (r *RateLimiter) Wait(ctx context.Context) error {
	wait, reservation, err := r.reserve(ctx)
	if err != nil {
		return err
	}

	if wait <= 0 {
		return nil
	}

	timer := time.NewTimer(wait)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		r.rollback(reservation)

		return fmt.Errorf("rate limiter wait canceled: %w", ctx.Err())
	case <-timer.C:
		return nil
	}
}

func (r *RateLimiter) Reserve(n int) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if n <= 0 || r.interval <= 0 {
		return
	}

	next := time.Now()

	if !r.last.IsZero() {
		if allowed := r.last.Add(r.interval); allowed.After(next) {
			next = allowed
		}
	}

	r.last = next.Add(time.Duration(n-1) * r.interval)

	r.seq += uint64(n)
}

func (r *RateLimiter) reserve(ctx context.Context) (time.Duration, rateLimiterReservation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return 0, rateLimiterReservation{}, fmt.Errorf("rate limiter wait canceled: %w", err)
	}

	if r.interval <= 0 {
		return 0, rateLimiterReservation{}, nil
	}

	now := time.Now()
	next := now

	if !r.last.IsZero() {
		if allowed := r.last.Add(r.interval); allowed.After(now) {
			next = allowed
		}
	}

	reservation := rateLimiterReservation{previous: r.last}

	r.last = next
	r.seq++

	reservation.seq = r.seq

	return next.Sub(now), reservation, nil
}

func (r *RateLimiter) rollback(reservation rateLimiterReservation) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.seq == reservation.seq {
		r.last = reservation.previous
		r.seq++
	}
}
