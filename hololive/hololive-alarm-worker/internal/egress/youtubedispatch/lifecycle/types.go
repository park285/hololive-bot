// Package lifecycle은 YouTube 전송 수명주기의 값과 순수 정책을 소유한다.
// DB·시계·제공자·로그·메트릭에 의존하지 않는다.
package lifecycle

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
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

func NewReason(value string) (Reason, error) {
	normalized := strings.TrimSpace(value)
	if normalized == "" {
		return "", errors.New("lifecycle reason is empty")
	}

	if len(normalized) > 100 {
		return "", errors.New("lifecycle reason is too long")
	}

	return Reason(normalized), nil
}

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

// SendFence는 저장된 특정 SENDING 버전의 소유권이다.
type SendFence struct {
	deliveryID int64
	rowVersion int64
	lockedAt   time.Time
}

func NewSendFence(deliveryID, rowVersion int64, lockedAt time.Time) (SendFence, error) {
	canonicalLockedAt, err := validateFence(deliveryID, rowVersion, lockedAt)
	if err != nil {
		return SendFence{}, fmt.Errorf("new send fence: %w", err)
	}

	return SendFence{deliveryID: deliveryID, rowVersion: rowVersion, lockedAt: canonicalLockedAt}, nil
}

func (f SendFence) DeliveryID() int64   { return f.deliveryID }
func (f SendFence) RowVersion() int64   { return f.rowVersion }
func (f SendFence) LockedAt() time.Time { return f.lockedAt }
func (f SendFence) Valid() bool         { return f.deliveryID > 0 && f.rowVersion > 0 && !f.lockedAt.IsZero() }

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

// AlarmClaimToken은 준비 단계에서 획득한 게시물 claim을 고정한다.
type AlarmClaimToken struct {
	kind         domain.OutboxKind
	postID       string
	authorizedAt time.Time
}

func NewAlarmClaimToken(kind domain.OutboxKind, postID string, authorizedAt time.Time) (AlarmClaimToken, error) {
	if kind != domain.OutboxKindNewShort && kind != domain.OutboxKindCommunityPost {
		return AlarmClaimToken{}, fmt.Errorf("new alarm claim token: unsupported kind %q", kind)
	}

	normalizedPostID := strings.TrimSpace(postID)
	if normalizedPostID == "" {
		return AlarmClaimToken{}, errors.New("new alarm claim token: post id is empty")
	}

	canonicalAuthorizedAt, err := CanonicalTime(authorizedAt)
	if err != nil {
		return AlarmClaimToken{}, fmt.Errorf("new alarm claim token: authorized at: %w", err)
	}

	return AlarmClaimToken{kind: kind, postID: normalizedPostID, authorizedAt: canonicalAuthorizedAt}, nil
}

func (t AlarmClaimToken) Kind() domain.OutboxKind { return t.kind }
func (t AlarmClaimToken) PostID() string          { return t.postID }
func (t AlarmClaimToken) AuthorizedAt() time.Time { return t.authorizedAt }

// TrackingRequirementKind는 게시물 종료 처리 규칙의 집합이다.
type TrackingRequirementKind uint8

const (
	TrackingNone TrackingRequirementKind = iota + 1
	TrackingClaimOrAlreadySent
	TrackingAlreadySent
)

type TrackingRequirement interface {
	Kind() TrackingRequirementKind
	trackingRequirement()
}

type NoTracking struct{}

func (NoTracking) Kind() TrackingRequirementKind { return TrackingNone }
func (NoTracking) trackingRequirement()          {}

type RequireClaimOrAlreadySent struct {
	token AlarmClaimToken
}

func NewRequireClaimOrAlreadySent(token AlarmClaimToken) (RequireClaimOrAlreadySent, error) {
	if token.postID == "" || token.authorizedAt.IsZero() {
		return RequireClaimOrAlreadySent{}, errors.New("require claim or already sent: invalid claim token")
	}

	return RequireClaimOrAlreadySent{token: token}, nil
}

func (r RequireClaimOrAlreadySent) Kind() TrackingRequirementKind { return TrackingClaimOrAlreadySent }
func (r RequireClaimOrAlreadySent) Token() AlarmClaimToken        { return r.token }
func (RequireClaimOrAlreadySent) trackingRequirement()            {}

type RequireAlreadySent struct {
	kind   domain.OutboxKind
	postID string
}

func NewRequireAlreadySent(kind domain.OutboxKind, postID string) (RequireAlreadySent, error) {
	if kind != domain.OutboxKindNewShort && kind != domain.OutboxKindCommunityPost {
		return RequireAlreadySent{}, fmt.Errorf("require already sent: unsupported kind %q", kind)
	}

	normalizedPostID := strings.TrimSpace(postID)
	if normalizedPostID == "" {
		return RequireAlreadySent{}, errors.New("require already sent: post id is empty")
	}

	return RequireAlreadySent{kind: kind, postID: normalizedPostID}, nil
}

func (r RequireAlreadySent) Kind() TrackingRequirementKind { return TrackingAlreadySent }
func (r RequireAlreadySent) OutboxKind() domain.OutboxKind { return r.kind }
func (r RequireAlreadySent) PostID() string                { return r.postID }
func (RequireAlreadySent) trackingRequirement()            {}

// ProviderOutcomeKind는 외부 처리 결과가 미확정일 때 재시도 가능 실패로 오인하지 않게 한다.
type ProviderOutcomeKind uint8

const (
	ProviderDelivered ProviderOutcomeKind = iota + 1
	ProviderKnownNotDeliveredRetryable
	ProviderKnownNotDeliveredPermanent
	ProviderOutcomeUnknown
)

type ProviderOutcome struct {
	kind       ProviderOutcomeKind
	reason     Reason
	retryAfter time.Duration
}

func NewProviderOutcome(kind ProviderOutcomeKind, reason Reason, retryAfter time.Duration) (ProviderOutcome, error) {
	if kind < ProviderDelivered || kind > ProviderOutcomeUnknown {
		return ProviderOutcome{}, errors.New("provider outcome kind is invalid")
	}

	if retryAfter < 0 {
		return ProviderOutcome{}, errors.New("provider retry after is negative")
	}

	if kind == ProviderDelivered {
		if reason != "" || retryAfter != 0 {
			return ProviderOutcome{}, errors.New("delivered provider outcome cannot include failure metadata")
		}
	} else if reason == "" {
		return ProviderOutcome{}, errors.New("failed provider outcome requires a reason")
	}

	if kind != ProviderKnownNotDeliveredRetryable && retryAfter != 0 {
		return ProviderOutcome{}, errors.New("only retryable known-not-delivered outcome can include retry after")
	}

	return ProviderOutcome{kind: kind, reason: reason, retryAfter: retryAfter}, nil
}

func (o ProviderOutcome) Kind() ProviderOutcomeKind { return o.kind }
func (o ProviderOutcome) Reason() Reason            { return o.reason }
func (o ProviderOutcome) RetryAfter() time.Duration { return o.retryAfter }
func (o ProviderOutcome) AllowsFallback(enabled bool) bool {
	return enabled && o.kind == ProviderKnownNotDeliveredPermanent
}
