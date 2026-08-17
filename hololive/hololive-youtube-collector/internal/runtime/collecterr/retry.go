package collecterr

import (
	"fmt"
	"time"
)

type RetryHintKind string

const (
	RetryDefault RetryHintKind = "DEFAULT"
	RetryAfter   RetryHintKind = "AFTER"
	RetryAt      RetryHintKind = "AT"
)

type RetryHint struct {
	kind  RetryHintKind
	after time.Duration
	at    time.Time
}

func defaultRetryHint() RetryHint {
	return RetryHint{kind: RetryDefault}
}

func NewRetryAfterHint(after time.Duration) (RetryHint, error) {
	hint := RetryHint{kind: RetryAfter, after: after}
	if err := hint.Validate(); err != nil {
		return RetryHint{}, err
	}
	return hint, nil
}

func NewRetryAtHint(at time.Time) (RetryHint, error) {
	if at.IsZero() {
		return RetryHint{}, fmt.Errorf("validate retry hint: timestamp is zero")
	}
	hint := RetryHint{kind: RetryAt, at: at.UTC()}
	if err := hint.Validate(); err != nil {
		return RetryHint{}, err
	}
	return hint, nil
}

func (h RetryHint) Kind() RetryHintKind  { return h.kind }
func (h RetryHint) After() time.Duration { return h.after }
func (h RetryHint) At() time.Time        { return h.at }

func (h RetryHint) Validate() error {
	switch h.kind {
	case RetryDefault:
		return h.validateDefault()
	case RetryAfter:
		return h.validateAfter()
	case RetryAt:
		return h.validateAt()
	default:
		return fmt.Errorf("validate retry hint: unknown kind %q", h.kind)
	}
}

func (h RetryHint) validateDefault() error {
	if h.after != 0 || !h.at.IsZero() {
		return fmt.Errorf("validate retry hint: DEFAULT requires a zero payload")
	}
	return nil
}

func (h RetryHint) validateAfter() error {
	if h.after <= 0 || h.after%time.Millisecond != 0 || !h.at.IsZero() {
		return fmt.Errorf("validate retry hint: AFTER requires a positive millisecond-aligned duration")
	}
	return nil
}

func (h RetryHint) validateAt() error {
	if h.at.IsZero() || h.at.Location() != time.UTC || h.after != 0 {
		return fmt.Errorf("validate retry hint: AT requires a UTC timestamp")
	}
	return nil
}

func CooldownUntil(message string, retryAt time.Time) error {
	hint, err := NewRetryAtHint(retryAt)
	if err != nil {
		return New(Cooldown, ClassCooldown, message)
	}
	return WithRetry(New(Cooldown, ClassCooldown, message), hint)
}
