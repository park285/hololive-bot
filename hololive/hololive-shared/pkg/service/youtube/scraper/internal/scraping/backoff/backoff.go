package backoff

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

	hardErrors   int
	hardCooldown time.Time

	transientErrors   int
	transientCooldown time.Time

	jitterPortion float64
	jitterRNG     *rand.Rand
}

type BackoffOption func(*BackoffState)

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
		//nolint:gosec
		return rand.New(rand.NewSource(time.Now().UnixNano()))
	}
	//nolint:gosec
	return rand.New(rand.NewSource(int64(binary.LittleEndian.Uint64(seed[:]))))
}

func (b *BackoffState) applyJitter(cooldown time.Duration) time.Duration {
	if cooldown <= 0 || b.jitterPortion <= 0 || b.jitterRNG == nil {
		return cooldown
	}
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

	return max(base, clampCooldown(suggested, minValue, maxValue))
}

func laterDeadline(current, candidate time.Time) time.Time {
	if current.After(candidate) {
		return current
	}
	return candidate
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
