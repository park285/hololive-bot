package lifecycle

import (
	"errors"
	"time"
)

type DeliveryStatus string

const (
	StatusPending     DeliveryStatus = "PENDING"
	StatusSending     DeliveryStatus = "SENDING"
	StatusSent        DeliveryStatus = "SENT"
	StatusFailed      DeliveryStatus = "FAILED"
	StatusQuarantined DeliveryStatus = "QUARANTINED"
)

func (s DeliveryStatus) Valid() bool {
	switch s {
	case StatusPending, StatusSending, StatusSent, StatusFailed, StatusQuarantined:
		return true
	default:
		return false
	}
}

type LedgerStatus string

const (
	LedgerSent        LedgerStatus = "SENT"
	LedgerQuarantined LedgerStatus = "QUARANTINED"
)

func (s LedgerStatus) Valid() bool {
	return s == LedgerSent || s == LedgerQuarantined
}

type Event uint8

const (
	EventBeginSending Event = iota + 1
	EventPreparationFailure
	EventKnownNotDelivered
	EventProviderDelivered
	EventStaleSending
	EventLogicalFulfilled
	EventLogicalUnresolved
	EventFollowerDeferred
	EventRevive
)

type FailureKind uint8

const (
	FailureRetryable FailureKind = iota + 1
	FailurePermanent
	FailureOutcomeUnknown
)

type RuleID string

const (
	RuleRetryScheduled       RuleID = "youtube_delivery.retry_scheduled"
	RuleRetryExhausted       RuleID = "youtube_delivery.retry_exhausted"
	RulePermanentFailure     RuleID = "youtube_delivery.permanent_failure"
	RuleLogicalGroupRevived  RuleID = "youtube_delivery.logical_group_revived"
	RuleLogicalGroupDeferred RuleID = "youtube_delivery.logical_group_deferred"
)

type Reason string

func CanonicalTime(value time.Time) (time.Time, error) {
	if value.IsZero() {
		return time.Time{}, errors.New("canonical lifecycle time is zero")
	}

	return value.UTC().Truncate(time.Microsecond), nil
}

type DueMode uint8

const (
	DueKeep DueMode = iota
	DueDelay
)

type DueSpec struct {
	Mode  DueMode
	Delay time.Duration
	Floor time.Time
}

func DelayedDue(delay time.Duration, floor time.Time) DueSpec {
	return DueSpec{Mode: DueDelay, Delay: delay, Floor: floor}
}
