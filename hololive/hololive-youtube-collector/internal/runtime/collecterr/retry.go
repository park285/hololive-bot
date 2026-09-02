package collecterr

import (
	"errors"
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
		return RetryHint{}, fmt.Errorf("validate: %w", err)
	}

	return hint, nil
}

func NewRetryAtHint(at time.Time) (RetryHint, error) {
	if at.IsZero() {
		return RetryHint{}, errors.New("validate retry hint: timestamp is zero")
	}

	hint := RetryHint{kind: RetryAt, at: at.UTC()}
	if err := hint.Validate(); err != nil {
		return RetryHint{}, fmt.Errorf("validate: %w", err)
	}

	return hint, nil
}

func (h RetryHint) Kind() RetryHintKind  { return h.kind }
func (h RetryHint) After() time.Duration { return h.after }
func (h RetryHint) At() time.Time        { return h.at }

func (h RetryHint) Validate() error {
	switch h.kind {
	case RetryDefault:
		return errors.Join(h.validateDefaultHint())
	case RetryAfter:
		return errors.Join(h.validateAfterHint())
	case RetryAt:
		return errors.Join(h.validateAtHint())
	default:
		return fmt.Errorf("validate retry hint: unknown kind %q", h.kind)
	}
}

func (h RetryHint) validateDefaultHint() error {
	if err := h.validateDefault(); err != nil {
		return fmt.Errorf("validate default: %w", err)
	}

	return nil
}

func (h RetryHint) validateAfterHint() error {
	if err := h.validateAfter(); err != nil {
		return fmt.Errorf("validate after: %w", err)
	}

	return nil
}

func (h RetryHint) validateAtHint() error {
	if err := h.validateAt(); err != nil {
		return fmt.Errorf("validate at: %w", err)
	}

	return nil
}

func (h RetryHint) validateDefault() error {
	if h.after != 0 || !h.at.IsZero() {
		return errors.New("validate retry hint: DEFAULT requires a zero payload")
	}

	return nil
}

func (h RetryHint) validateAfter() error {
	if h.after <= 0 || h.after%time.Millisecond != 0 || !h.at.IsZero() {
		return errors.New("validate retry hint: AFTER requires a positive millisecond-aligned duration")
	}

	return nil
}

func (h RetryHint) validateAt() error {
	if h.at.IsZero() || h.at.Location() != time.UTC || h.after != 0 {
		return errors.New("validate retry hint: AT requires a UTC timestamp")
	}

	return nil
}

func CooldownUntil(message string, retryAt time.Time) error {
	hint, err := NewRetryAtHint(retryAt)
	if err != nil {
		return New(Cooldown, ClassCooldown, message)
	}

	if err := WithRetry(New(Cooldown, ClassCooldown, message), hint); err != nil {
		return fmt.Errorf("with retry: %w", err)
	}

	return nil
}
