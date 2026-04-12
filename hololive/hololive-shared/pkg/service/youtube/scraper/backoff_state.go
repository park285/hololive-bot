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
	b.mu.Lock()
	defer b.mu.Unlock()

	b.hardErrors++

	var cooldown time.Duration
	switch {
	case b.hardErrors >= 5:
		cooldown = 6 * time.Hour
	case b.hardErrors >= 4:
		cooldown = 4 * time.Hour
	case b.hardErrors >= 3:
		cooldown = 2 * time.Hour
	case b.hardErrors >= 2:
		cooldown = 1 * time.Hour
	default:
		cooldown = 30 * time.Minute
	}

	b.hardCooldown = time.Now().Add(cooldown)
	slog.Warn("Hard backoff activated",
		"consecutive_errors", b.hardErrors,
		"cooldown", cooldown)
}

func (b *BackoffState) RecordTransientError() {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.transientErrors++

	var cooldown time.Duration
	switch {
	case b.transientErrors >= 3:
		cooldown = 10 * time.Minute
	case b.transientErrors >= 2:
		cooldown = 3 * time.Minute
	default:
		cooldown = 30 * time.Second
	}

	b.transientCooldown = time.Now().Add(cooldown)
	slog.Warn("Transient backoff activated",
		"consecutive_transient_errors", b.transientErrors,
		"cooldown", cooldown)
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
