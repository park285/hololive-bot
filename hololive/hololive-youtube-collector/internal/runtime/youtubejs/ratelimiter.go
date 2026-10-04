package youtubejs

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// RateLimiter는 이 collector process가 youtube.js helper에 보내는 RPC의 시작 간격을
// YOUTUBE_COLLECTOR_REQUEST_INTERVAL_SECONDS 이상으로 벌린다. 다른 process·host와 예산을 나누지 않는 local 간격이다.
// Wait는 다음 허용 시각을 예약한 뒤 그때까지 기다린다. 기다리는 중 ctx가 끝나면 그 뒤에 다른 예약이 없을 때만 예약을
// 되돌려, 취소된 호출이 다음 호출의 간격을 밀어내지 않게 한다. 요청 간격이 0 이하면 기다리지 않는다.
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
