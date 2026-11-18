package apiclient

import (
	"math"
	"sync"
	"sync/atomic"
	"time"
)

type circuitBreaker struct {
	transitionMu sync.Mutex
	open         atomic.Bool
	openedAt     atomic.Value
	failures     atomic.Int32
	threshold    int32
	resetTimeout time.Duration
}

func newCircuitBreaker(threshold int, resetTimeout time.Duration) *circuitBreaker {
	b := &circuitBreaker{
		threshold:    normalizeBreakerThreshold(threshold),
		resetTimeout: resetTimeout,
	}
	b.openedAt.Store(time.Time{})

	return b
}

func normalizeBreakerThreshold(threshold int) int32 {
	switch {
	case threshold <= 0:
		return 1
	case threshold > math.MaxInt32:
		return math.MaxInt32
	default:
		return int32(threshold)
	}
}

func (b *circuitBreaker) Allow() bool {
	if b == nil {
		return true
	}

	if !b.open.Load() {
		return true
	}

	openedAt := b.openedAtTime()
	if time.Since(openedAt) > b.resetTimeout {
		b.transitionMu.Lock()
		defer b.transitionMu.Unlock()

		if !b.open.Load() {
			return true
		}

		if time.Since(b.openedAtTime()) <= b.resetTimeout {
			return false
		}

		b.failures.Store(0)
		b.open.Store(false)

		return true
	}

	return false
}

func (b *circuitBreaker) RecordSuccess() {
	if b == nil {
		return
	}

	b.failures.Store(0)
}

func (b *circuitBreaker) RecordFailure() bool {
	if b == nil {
		return false
	}

	if b.open.Load() {
		return false
	}

	count := b.failures.Add(1)
	if count >= b.threshold {
		b.transitionMu.Lock()
		defer b.transitionMu.Unlock()

		if b.open.Load() {
			return false
		}

		b.openedAt.Store(time.Now())
		b.open.Store(true)

		return true
	}

	return false
}

func (b *circuitBreaker) IsOpen() bool {
	if b == nil {
		return false
	}

	if !b.open.Load() {
		return false
	}

	openedAt := b.openedAtTime()

	return time.Since(openedAt) <= b.resetTimeout
}

func (b *circuitBreaker) Failures() int32 {
	if b == nil {
		return 0
	}

	return b.failures.Load()
}

func (b *circuitBreaker) RetryAfter() time.Duration {
	if b == nil {
		return 0
	}

	if !b.open.Load() {
		return 0
	}

	openedAt := b.openedAtTime()
	remaining := b.resetTimeout - time.Since(openedAt)

	if remaining < 0 {
		return 0
	}

	return remaining
}

func (b *circuitBreaker) openedAtTime() time.Time {
	openedAt, ok := b.openedAt.Load().(time.Time)
	if !ok {
		return time.Time{}
	}

	return openedAt
}
