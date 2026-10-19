// Package lifecycle은 YouTube 전송 수명주기의 값과 순수 정책을 소유한다.
// DB·시계·제공자·로그·메트릭에 의존하지 않는다.
package lifecycle

import (
	"errors"
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

// DueMode는 전이가 next_attempt_at을 정하는 방식이다. 0 값은 기존 값을 유지한다.
type DueMode uint8

const (
	DueKeep DueMode = iota
	DueDelay
)

// DueSpec은 next_attempt_at을 절대 시각 대신 DB 시계 기준 지연으로 표현한다. 전이 SQL이
// GREATEST(clock + Delay, Floor)로 계산하며, Floor는 DB 행에서 온 시각(owner next_attempt_at, in-flight lease 만료)만
// 쓴다. 0 값 Floor는 하한이 없다는 뜻이다.
type DueSpec struct {
	Mode  DueMode
	Delay time.Duration
	Floor time.Time
}

// DelayedDue는 DB 시계에서 delay 뒤이면서 floor보다 이르지 않은 next_attempt_at을 요구한다.
func DelayedDue(delay time.Duration, floor time.Time) DueSpec {
	return DueSpec{Mode: DueDelay, Delay: delay, Floor: floor}
}
