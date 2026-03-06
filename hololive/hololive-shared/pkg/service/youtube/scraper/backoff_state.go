package scraper

import (
	"log/slog"
	"sync"
	"time"
)

// BackoffState: 듀얼 상태 지수 백오프 관리 (hard: 429/403, transient: 5xx)
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

// RecordError: hard 에러 기록 (429/403 전용, 장기 쿨다운)
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

// RecordTransientError: transient 에러 기록 (5xx 전용, 경량 쿨다운)
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

// RecordSuccess: 양쪽 상태 모두 리셋
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

// HardCooldownRemaining: hard 쿨다운 잔여 시간 (fetchPageOnce 전용)
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

// TransientCooldownRemaining: transient 쿨다운 잔여 시간 (fetchPage 진입 전용)
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

// CooldownRemaining: max(hard, transient) 쿨다운 반환
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

// IsInCooldown: 쿨다운 중인지 확인
func (b *BackoffState) IsInCooldown() bool {
	return b.CooldownRemaining() > 0
}
