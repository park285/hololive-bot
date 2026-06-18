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

package util

import (
	"math"
	"sync/atomic"
	"time"
)

// Breaker는 atomic 연산 기반 경량 circuit breaker입니다.
// 시간 기반 reset 전용으로, HALF_OPEN 상태 없이 open→closed 직접 전이합니다.
// 로그는 provider가 책임지므로 Breaker 내부에서 emit하지 않습니다.
type Breaker struct {
	open         atomic.Bool
	openedAt     atomic.Value // time.Time
	failures     atomic.Int32
	threshold    int32
	resetTimeout time.Duration
}

// NewBreaker는 새 Breaker를 생성합니다.
func NewBreaker(threshold int, resetTimeout time.Duration) *Breaker {
	b := &Breaker{
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
// open 상태이고 resetTimeout이 경과했으면 reset(open=false, failures=0) 후 true를 반환합니다.
// open 상태이고 미경과면 false를 반환합니다.
// closed 상태면 true를 반환합니다.
// nil receiver는 항상 true(허용)를 반환합니다.
func (b *Breaker) Allow() bool {
	if b == nil {
		return true
	}
	if !b.open.Load() {
		return true
	}

	openedAt := b.openedAtTime()
	if time.Since(openedAt) > b.resetTimeout {
		// CAS로 reset 전이를 원자화: 단일 goroutine만 failures를 0으로 만든다.
		if b.open.CompareAndSwap(true, false) {
			b.failures.Store(0)
		}
		return true
	}

	return false
}

// RecordSuccess는 실패 카운터를 0으로 초기화합니다.
// open 상태는 시간 기반 reset(Allow 호출)으로만 해제됩니다.
func (b *Breaker) RecordSuccess() {
	if b == nil {
		return
	}
	b.failures.Store(0)
}

// RecordFailure는 실패를 기록합니다.
// 이번 호출로 closed→open 전이가 발생했으면 true를 반환합니다.
// 이미 open 상태거나 threshold 미달이면 false를 반환합니다.
// open 전이 시에만 openedAt을 갱신하므로 연속 실패가 resetTimeout을 밀지 않습니다.
func (b *Breaker) RecordFailure() bool {
	if b == nil {
		return false
	}
	if b.open.Load() {
		// 이미 open: openedAt 갱신 금지(타이머 밀림 방지)
		return false
	}
	count := b.failures.Add(1)
	if count >= b.threshold {
		// CAS로 open 전이를 원자화: 동시 진입해도 단 하나의 goroutine만
		// openedAt을 설정하므로 타이머 밀림·중복 전이가 없다.
		if b.open.CompareAndSwap(false, true) {
			b.openedAt.Store(time.Now())
			return true
		}
	}
	return false
}

// IsOpen은 현재 circuit 상태를 반환합니다. Allow()와 달리 side-effect가 없습니다.
// nil receiver는 false를 반환합니다.
func (b *Breaker) IsOpen() bool {
	if b == nil {
		return false
	}
	if !b.open.Load() {
		return false
	}
	openedAt := b.openedAtTime()
	return time.Since(openedAt) <= b.resetTimeout
}

// Failures는 현재 누적 실패 카운트를 반환합니다.
// provider가 로그에 failure_count를 기록할 때 사용합니다.
func (b *Breaker) Failures() int32 {
	if b == nil {
		return 0
	}
	return b.failures.Load()
}

// RetryAfter는 circuit이 열린 경우 남은 대기 시간을 반환합니다.
// closed 상태거나 resetTimeout이 이미 경과했으면 0을 반환합니다.
func (b *Breaker) RetryAfter() time.Duration {
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

// SetOpenedAtForTest는 테스트에서 openedAt을 강제 설정합니다.
// 프로덕션 코드에서 호출하지 마세요.
func (b *Breaker) SetOpenedAtForTest(t time.Time) {
	if b == nil {
		return
	}
	b.openedAt.Store(t)
}

func (b *Breaker) openedAtTime() time.Time {
	openedAt, ok := b.openedAt.Load().(time.Time)
	if !ok {
		return time.Time{}
	}
	return openedAt
}
