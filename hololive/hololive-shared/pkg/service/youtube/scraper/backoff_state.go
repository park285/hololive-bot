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

package scraper

import (
	"log/slog"
	"sync"
	"time"
)

const (
	minSuggestedHardCooldown      = 30 * time.Second
	maxSuggestedHardCooldown      = 6 * time.Hour
	minSuggestedTransientCooldown = 5 * time.Second
	maxSuggestedTransientCooldown = 10 * time.Minute
)

type BackoffState struct {
	mu sync.Mutex

	// hard: 429/403 전용 (장기 쿨다운)
	hardErrors   int
	hardCooldown time.Time

	// transient: 5xx 전용 (경량 쿨다운)
	transientErrors   int
	transientCooldown time.Time
}

func NewBackoffState() *BackoffState {
	return &BackoffState{}
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
	b.transientCooldown = laterDeadline(b.transientCooldown, now.Add(cooldown))
	slog.Warn("Transient backoff activated",
		"consecutive_transient_errors", b.transientErrors,
		"cooldown", cooldown,
		"suggested_cooldown", suggested)
}

func hardCooldownForErrorCount(errors int) time.Duration {
	switch {
	case errors >= 5:
		return 6 * time.Hour
	case errors >= 4:
		return 4 * time.Hour
	case errors >= 3:
		return 2 * time.Hour
	case errors >= 2:
		return 1 * time.Hour
	default:
		return 30 * time.Minute
	}
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
