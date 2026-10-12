// Package lifecycle은 YouTube 전송 수명주기의 값과 순수 정책을 소유한다.
// DB·시계·제공자·로그·메트릭에 의존하지 않는다.
package lifecycle

import (
	"errors"
	"fmt"
	"time"
)

// DeliveryStatus는 개별 전송의 상태 집합이다.
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

// LedgerStatus는 되돌리지 않는 논리 전송의 종단 증거다.
type LedgerStatus string

const (
	LedgerSent        LedgerStatus = "SENT"
	LedgerQuarantined LedgerStatus = "QUARANTINED"
)

func (s LedgerStatus) Valid() bool {
	return s == LedgerSent || s == LedgerQuarantined
}

// Event는 수명주기 전이를 일으킨 사건을 구분한다.
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

// FailureKind는 재시도 가능·영구·결과 미확정 실패를 구분한다.
type FailureKind uint8

const (
	FailureRetryable FailureKind = iota + 1
	FailurePermanent
	FailureOutcomeUnknown
)

// RuleID는 감사·메트릭에 사용하는 정책 결정 식별자다.
type RuleID string

const (
	RuleRetryScheduled       RuleID = "youtube_delivery.retry_scheduled"
	RuleRetryExhausted       RuleID = "youtube_delivery.retry_exhausted"
	RulePermanentFailure     RuleID = "youtube_delivery.permanent_failure"
	RuleLogicalGroupRevived  RuleID = "youtube_delivery.logical_group_revived"
	RuleLogicalGroupDeferred RuleID = "youtube_delivery.logical_group_deferred"
)

// Reason은 제한된 실패 분류이며 원본 페이로드를 포함하면 안 된다.
type Reason string

// CanonicalTime은 PostgreSQL에 저장되는 시간 표현을 적용한다.
func CanonicalTime(value time.Time) (time.Time, error) {
	if value.IsZero() {
		return time.Time{}, errors.New("canonical lifecycle time is zero")
	}

	return value.UTC().Truncate(time.Microsecond), nil
}

// PreparationLease는 claim한 특정 PENDING 버전의 소유권이다.
type PreparationLease struct {
	deliveryID int64
	rowVersion int64
	lockedAt   time.Time
}

func NewPreparationLease(deliveryID, rowVersion int64, lockedAt time.Time) (PreparationLease, error) {
	canonicalLockedAt, err := validateFence(deliveryID, rowVersion, lockedAt)
	if err != nil {
		return PreparationLease{}, fmt.Errorf("new preparation lease: %w", err)
	}

	return PreparationLease{deliveryID: deliveryID, rowVersion: rowVersion, lockedAt: canonicalLockedAt}, nil
}

func (l PreparationLease) DeliveryID() int64   { return l.deliveryID }
func (l PreparationLease) RowVersion() int64   { return l.rowVersion }
func (l PreparationLease) LockedAt() time.Time { return l.lockedAt }
func (l PreparationLease) Valid() bool {
	return l.deliveryID > 0 && l.rowVersion > 0 && !l.lockedAt.IsZero()
}

func validateFence(deliveryID, rowVersion int64, lockedAt time.Time) (time.Time, error) {
	if deliveryID <= 0 {
		return time.Time{}, errors.New("delivery id must be positive")
	}

	if rowVersion <= 0 {
		return time.Time{}, errors.New("row version must be positive")
	}

	canonicalLockedAt, err := CanonicalTime(lockedAt)
	if err != nil {
		return time.Time{}, fmt.Errorf("locked at: %w", err)
	}

	return canonicalLockedAt, nil
}
