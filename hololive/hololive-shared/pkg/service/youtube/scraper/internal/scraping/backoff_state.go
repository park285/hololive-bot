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
	crand "crypto/rand"
	"encoding/binary"
	"log/slog"
	"math/rand"
	"sync"
	"time"
)

const (
	minSuggestedHardCooldown      = 30 * time.Second
	maxSuggestedHardCooldown      = 6 * time.Hour
	minSuggestedTransientCooldown = 5 * time.Second
	maxSuggestedTransientCooldown = 10 * time.Minute

	maxCooldownJitterPortion = 0.5
)

type BackoffState struct {
	mu sync.Mutex

	// hard: 429/403 전용 (장기 쿨다운)
	hardErrors   int
	hardCooldown time.Time

	// transient: 5xx 전용 (경량 쿨다운)
	transientErrors   int
	transientCooldown time.Time

	// jitterPortion: ±portion 비율의 jitter (0이면 비활성)
	jitterPortion float64
	jitterRNG     *rand.Rand
}

type BackoffOption func(*BackoffState)

// WithCooldownJitter: hard/transient 쿨다운에 ±portion 비율의 jitter 적용.
// 분산 환경에서 다수 인스턴스가 동일 시각에 쿨다운에서 깨어나 같은 retry를 발사하는 thundering herd를 완화한다.
// portion이 0이면 jitter 비활성. portion은 [0, 0.5]로 clamp.
func WithCooldownJitter(portion float64) BackoffOption {
	return func(b *BackoffState) {
		if portion < 0 {
			portion = 0
		}
		if portion > maxCooldownJitterPortion {
			portion = maxCooldownJitterPortion
		}
		b.jitterPortion = portion
		if portion > 0 && b.jitterRNG == nil {
			b.jitterRNG = newBackoffJitterRNG()
		}
	}
}

func NewBackoffState(opts ...BackoffOption) *BackoffState {
	bs := &BackoffState{}
	for _, opt := range opts {
		opt(bs)
	}
	return bs
}

func newBackoffJitterRNG() *rand.Rand {
	var seed [8]byte
	if _, err := crand.Read(seed[:]); err != nil {
		//nolint:gosec // jitter용 비보안 난수.
		return rand.New(rand.NewSource(time.Now().UnixNano()))
	}
	//nolint:gosec // jitter용 비보안 난수.
	return rand.New(rand.NewSource(int64(binary.LittleEndian.Uint64(seed[:]))))
}

func (b *BackoffState) applyJitter(cooldown time.Duration) time.Duration {
	if cooldown <= 0 || b.jitterPortion <= 0 || b.jitterRNG == nil {
		return cooldown
	}
	// caller가 b.mu lock 보유한 상태로 호출. jitterRNG 동시 접근 직렬화.
	delta := (b.jitterRNG.Float64()*2 - 1) * b.jitterPortion
	scaled := float64(cooldown) * (1 + delta)
	if scaled <= 0 {
		return cooldown
	}
	return time.Duration(scaled)
}

func (b *BackoffState) RecordError() {
	b.RecordErrorWithSuggestedCooldown(0)
}

func (b *BackoffState) RecordErrorWithSuggestedCooldown(suggested time.Duration) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.hardErrors++

	now := time.Now()
	cooldown := resolveCooldown(
		hardCooldownForErrorCount(b.hardErrors),
		suggested,
		minSuggestedHardCooldown,
		maxSuggestedHardCooldown,
	)
	cooldown = b.applyJitter(cooldown)
	b.hardCooldown = laterDeadline(b.hardCooldown, now.Add(cooldown))
	slog.Warn("Hard backoff activated",
		"consecutive_errors", b.hardErrors,
		"cooldown", cooldown,
		"suggested_cooldown", suggested)
}

func (b *BackoffState) RecordTransientError() {
	b.RecordTransientErrorWithSuggestedCooldown(0)
}

func (b *BackoffState) RecordTransientErrorWithSuggestedCooldown(suggested time.Duration) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.transientErrors++

	now := time.Now()
	cooldown := resolveCooldown(
		transientCooldownForErrorCount(b.transientErrors),
		suggested,
		minSuggestedTransientCooldown,
		maxSuggestedTransientCooldown,
	)
	cooldown = b.applyJitter(cooldown)
	b.transientCooldown = laterDeadline(b.transientCooldown, now.Add(cooldown))
	slog.Warn("Transient backoff activated",
		"consecutive_transient_errors", b.transientErrors,
		"cooldown", cooldown,
		"suggested_cooldown", suggested)
}

func hardCooldownForErrorCount(errors int) time.Duration {
	thresholds := []struct {
		errors   int
		cooldown time.Duration
	}{
		{errors: 5, cooldown: 6 * time.Hour},
		{errors: 4, cooldown: 4 * time.Hour},
		{errors: 3, cooldown: 2 * time.Hour},
		{errors: 2, cooldown: 1 * time.Hour},
	}

	for _, threshold := range thresholds {
		if errors >= threshold.errors {
			return threshold.cooldown
		}
	}
	return 30 * time.Minute
}

func transientCooldownForErrorCount(errors int) time.Duration {
	switch {
	case errors >= 3:
		return 10 * time.Minute
	case errors >= 2:
		return 3 * time.Minute
	default:
		return 30 * time.Second
	}
}

func clampCooldown(value, minValue, maxValue time.Duration) time.Duration {
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}

func resolveCooldown(base, suggested, minValue, maxValue time.Duration) time.Duration {
	if suggested <= 0 {
		return base
	}

	return maxDuration(base, clampCooldown(suggested, minValue, maxValue))
}

func laterDeadline(current, candidate time.Time) time.Time {
	if current.After(candidate) {
		return current
	}
	return candidate
}

func maxDuration(a, b time.Duration) time.Duration {
	if a > b {
		return a
	}
	return b
}

func (b *BackoffState) RecordSuccess() {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.hardErrors > 0 || b.transientErrors > 0 {
		slog.Info("Backoff reset after success",
			"previous_hard_errors", b.hardErrors,
			"previous_transient_errors", b.transientErrors)
	}

	b.hardErrors = 0
	b.hardCooldown = time.Time{}
	b.transientErrors = 0
	b.transientCooldown = time.Time{}
}

func (b *BackoffState) HardCooldownRemaining() time.Duration {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.hardCooldown.IsZero() {
		return 0
	}

	remaining := time.Until(b.hardCooldown)
	if remaining <= 0 {
		b.hardCooldown = time.Time{}
		b.hardErrors = 0
		return 0
	}

	return remaining
}

func (b *BackoffState) TransientCooldownRemaining() time.Duration {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.transientCooldown.IsZero() {
		return 0
	}

	remaining := time.Until(b.transientCooldown)
	if remaining <= 0 {
		b.transientCooldown = time.Time{}
		b.transientErrors = 0
		return 0
	}

	return remaining
}

func (b *BackoffState) CooldownRemaining() time.Duration {
	b.mu.Lock()
	defer b.mu.Unlock()

	var hard, transient time.Duration

	if !b.hardCooldown.IsZero() {
		hard = time.Until(b.hardCooldown)
		if hard <= 0 {
			b.hardCooldown = time.Time{}
			b.hardErrors = 0
			hard = 0
		}
	}

	if !b.transientCooldown.IsZero() {
		transient = time.Until(b.transientCooldown)
		if transient <= 0 {
			b.transientCooldown = time.Time{}
			b.transientErrors = 0
			transient = 0
		}
	}

	if hard > transient {
		return hard
	}
	return transient
}

func (b *BackoffState) IsInCooldown() bool {
	return b.CooldownRemaining() > 0
}
