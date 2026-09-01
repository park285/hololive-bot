// Package lifecycle owns pure YouTube delivery lifecycle values and policy.
// It deliberately has no database, clock, provider, logging, or metrics dependency.
package lifecycle

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

// DeliveryStatus is the complete physical delivery state machine.
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

// LedgerStatus is monotonic logical terminal evidence.
type LedgerStatus string

const (
	LedgerSent        LedgerStatus = "SENT"
	LedgerQuarantined LedgerStatus = "QUARANTINED"
)

func (s LedgerStatus) Valid() bool {
	return s == LedgerSent || s == LedgerQuarantined
}

// Event identifies a lifecycle transition trigger without encoding policy in a string.
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

// FailureKind separates retryable, permanent, and indeterminate failures.
type FailureKind uint8

const (
	FailureRetryable FailureKind = iota + 1
	FailurePermanent
	FailureOutcomeUnknown
)

// RuleID is a stable policy decision identifier used by audit and metrics.
type RuleID string

const (
	RuleRetryScheduled       RuleID = "youtube_delivery.retry_scheduled"
	RuleRetryExhausted       RuleID = "youtube_delivery.retry_exhausted"
	RulePermanentFailure     RuleID = "youtube_delivery.permanent_failure"
	RuleLogicalGroupRevived  RuleID = "youtube_delivery.logical_group_revived"
	RuleLogicalGroupDeferred RuleID = "youtube_delivery.logical_group_deferred"
)

// Reason is a bounded semantic failure class. It must never contain raw payloads.
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

// CanonicalTime applies the timestamp representation persisted by PostgreSQL.
func CanonicalTime(value time.Time) (time.Time, error) {
	if value.IsZero() {
		return time.Time{}, errors.New("canonical lifecycle time is zero")
	}

	return value.UTC().Truncate(time.Microsecond), nil
}

// PreparationLease proves ownership of one exact claimed PENDING version.
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

// SendFence proves ownership of one exact durable SENDING version.
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

// AlarmClaimToken freezes the exact post-level claim acquired during preparation.
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

// TrackingRequirementKind is the closed set of post-level finalization rules.
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

// ProviderOutcomeKind prevents an indeterminate external effect from being
// treated as a known retryable failure.
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
