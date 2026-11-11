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

package apiclient

import (
	"math"
	"sync"
	"sync/atomic"
	"time"
)

// 시간 기반 reset 전용으로, HALF_OPEN 상태 없이 open→closed 직접 전이합니다. 로그는 provider가 책임지므로 circuitBreaker 내부에서 emit하지 않습니다.
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

// Allow는 요청 허용 여부를 반환하며, timeout 경과 시 reset side-effect를 수행합니다.
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

// 회로가 open 상태일 때는 시간 기반 reset(Allow 호출)으로만 해제됩니다.
func (b *circuitBreaker) RecordSuccess() {
	if b == nil {
		return
	}

	b.failures.Store(0)
}

// 이번 호출로 closed→open 전이가 발생했으면 true를 반환합니다. 이미 open 상태거나 threshold 미달이면 false를 반환합니다. 회로가 open으로 전이할 때에만 openedAt을
// 갱신하므로 연속 실패가 resetTimeout을 밀지 않습니다.
func (b *circuitBreaker) RecordFailure() bool {
	if b == nil {
		return false
	}

	if b.open.Load() {
		// 이미 open: openedAt 갱신 금지(타이머 밀림 방지)
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

// Allow()와 달리 side-effect가 없습니다.
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
