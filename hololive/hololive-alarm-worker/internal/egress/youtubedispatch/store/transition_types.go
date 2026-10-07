package store

import (
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/lifecycle"
	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/preparation"
	"github.com/kapu/hololive-shared/pkg/domain"
	ytcontentid "github.com/kapu/hololive-shared/pkg/service/youtube/contentid"
)

const (
	DeliveryStatusSending     domain.OutboxStatus = "SENDING"
	DeliveryStatusQuarantined domain.OutboxStatus = "QUARANTINED"
)

// ApplyOutcome은 불변 수명주기 명령 하나의 결과 집합이다.
type ApplyOutcome uint8

const (
	ApplyApplied ApplyOutcome = iota + 1
	ApplyConflict
	ApplyMissing
	ApplyIndeterminate
)

func (o ApplyOutcome) String() string {
	switch o {
	case ApplyApplied:
		return "applied"
	case ApplyConflict:
		return "conflict"
	case ApplyMissing:
		return "missing"
	case ApplyIndeterminate:
		return "indeterminate"
	default:
		return "invalid"
	}
}

// ApplyResult는 영속 전이 증거만 보고한다. 집계 projection은 수명주기 트랜잭션 밖에서 갱신한다.
type ApplyResult struct {
	Outcome            ApplyOutcome
	TouchedOutboxIDs   []int64
	Rules              []lifecycle.RuleID
	CommitAdjudication CommitAdjudication
}

type CommitAdjudication string

const (
	CommitConfirmedPost CommitAdjudication = "confirmed_post"
	CommitConfirmedPre  CommitAdjudication = "confirmed_pre"
	CommitMissing       CommitAdjudication = "missing"
	CommitConflict      CommitAdjudication = "conflict"
	CommitMixed         CommitAdjudication = "mixed"
	CommitIndeterminate CommitAdjudication = "indeterminate"
)

func newApplyResult(outcome ApplyOutcome, outboxIDs []int64) ApplyResult {
	return ApplyResult{Outcome: outcome, TouchedOutboxIDs: uniqueSortedInt64s(outboxIDs)}
}

func newApplyResultWithRules(outcome ApplyOutcome, outboxIDs []int64, rules []lifecycle.RuleID) ApplyResult {
	result := newApplyResult(outcome, outboxIDs)

	result.Rules = slices.Clone(rules)

	return result
}

func adjudicated(result ApplyResult, err error, adjudication CommitAdjudication) ApplyResult {
	if _, ok := errors.AsType[*commitResponseError](err); ok {
		result.CommitAdjudication = adjudication
	}

	return result
}

type commitResponseError struct {
	operation string
	err       error
}

func (e *commitResponseError) Error() string {
	return fmt.Sprintf("%s commit response: %v", e.operation, e.err)
}

func (e *commitResponseError) Unwrap() error { return e.err }

// AtomicityBreachError는 원본 상태에 명령 envelope가 섞였음을 나타낸다.
// 호출자가 추정으로 복구하거나 외부 부수 효과를 재실행하면 안 된다.
type AtomicityBreachError struct {
	Operation string
	Detail    string
}

func (e *AtomicityBreachError) Error() string {
	return fmt.Sprintf("%s atomicity breach: %s", e.Operation, e.Detail)
}

type transitionConflictError struct {
	operation string
	detail    string
}

func (e *transitionConflictError) Error() string {
	return fmt.Sprintf("%s conflict: %s", e.operation, e.detail)
}

type transitionMissingError struct {
	operation string
	detail    string
}

func (e *transitionMissingError) Error() string {
	return fmt.Sprintf("%s missing: %s", e.operation, e.detail)
}

type TransitionConfig struct {
	MaxRetries           int
	RetryBackoff         time.Duration
	LockTimeout          time.Duration
	ClaimFreshnessWindow time.Duration
	LogicalGroupLimit    int
}

type PrepareClaimsResult struct {
	ActiveRows       []domain.YouTubeNotificationDelivery
	TouchedOutboxIDs []int64
	Blocked          []BlockedLogicalGroup
	Resolutions      []preparation.ResolutionKind
}

type BlockedLogicalGroup struct {
	KeyHash string
	Reason  preparation.InvariantReason
}

type transitionRow struct {
	ID              int64
	OutboxID        int64
	RoomID          string
	Status          lifecycle.DeliveryStatus
	AttemptCount    int
	NextAttemptAt   time.Time
	CreatedAt       time.Time
	LockedAt        *time.Time
	SentAt          *time.Time
	Error           string
	RowVersion      int64
	Kind            domain.OutboxKind
	ChannelID       string
	ContentID       string
	Payload         string
	OutboxCreatedAt time.Time
	OutboxSentAt    *time.Time
}

func (r transitionRow) domainDelivery() domain.YouTubeNotificationDelivery {
	return domain.YouTubeNotificationDelivery{
		ID: r.ID, OutboxID: r.OutboxID, RoomID: r.RoomID,
		Status: domain.OutboxStatus(r.Status), AttemptCount: r.AttemptCount,
		NextAttemptAt: r.NextAttemptAt, CreatedAt: r.CreatedAt,
		LockedAt: cloneTimePtr(r.LockedAt), SentAt: cloneTimePtr(r.SentAt),
		Error: r.Error, RowVersion: r.RowVersion,
	}
}

func (r transitionRow) domainOutbox() domain.YouTubeNotificationOutbox {
	return domain.YouTubeNotificationOutbox{
		ID: r.OutboxID, Kind: r.Kind, ChannelID: r.ChannelID,
		ContentID: r.ContentID, Payload: r.Payload, CreatedAt: r.OutboxCreatedAt,
		SentAt: cloneTimePtr(r.OutboxSentAt),
	}
}

func (r transitionRow) logicalKey() (ytcontentid.LogicalKey, error) {
	key, err := ytcontentid.ResolveDeliveryKey(r.Kind, r.ContentID, r.Payload, r.RoomID)
	if err != nil {
		return ytcontentid.LogicalKey{}, fmt.Errorf("delivery %d logical key: %w", r.ID, err)
	}

	return key, nil
}

func (r transitionRow) snapshot(current bool) (preparation.DeliverySnapshot, error) {
	lockedAt := time.Time{}

	if r.LockedAt != nil {
		lockedAt = r.LockedAt.UTC()
	}

	var lease lifecycle.PreparationLease

	if current {
		var err error

		lease, err = lifecycle.NewPreparationLease(r.ID, r.RowVersion, lockedAt)
		if err != nil {
			return preparation.DeliverySnapshot{}, fmt.Errorf("delivery %d preparation lease: %w", r.ID, err)
		}
	}

	return preparation.DeliverySnapshot{
		DeliveryID: r.ID, OutboxID: r.OutboxID, Kind: r.Kind,
		ContentID: r.ContentID, Payload: r.Payload, RoomID: r.RoomID,
		Status: r.Status, AttemptCount: r.AttemptCount,
		NextAttemptAt: r.NextAttemptAt, CreatedAt: r.CreatedAt,
		LockedAt: lockedAt, RowVersion: r.RowVersion,
		InCurrentBatch: current, Lease: lease,
	}, nil
}

func transitionRowFromSnapshot(snapshot preparation.DeliverySnapshot, byID map[int64]transitionRow) (transitionRow, error) {
	row, ok := byID[snapshot.DeliveryID]
	if !ok {
		return transitionRow{}, fmt.Errorf("delivery %d snapshot row is absent", snapshot.DeliveryID)
	}

	return row, nil
}

type startedLogicalGroup struct {
	key           ytcontentid.LogicalKey
	ownerBefore   transitionRow
	ownerAfter    transitionRow
	followers     []transitionRow
	ledgerBefore  *DeliveryLedgerRecord
	providerOwner bool
}

type StartedOperation struct {
	groups    []startedLogicalGroup
	startedAt time.Time
}

func (o StartedOperation) Valid() bool {
	return len(o.groups) > 0 && !o.startedAt.IsZero()
}

func (o StartedOperation) OwnerCount() int {
	return len(o.groups)
}

func (o StartedOperation) ForOwner(deliveryID int64) (StartedOperation, error) {
	if !o.Valid() || deliveryID <= 0 {
		return StartedOperation{}, errors.New("select started operation owner: invalid input")
	}

	for i := range o.groups {
		if o.groups[i].ownerAfter.ID == deliveryID {
			return StartedOperation{groups: []startedLogicalGroup{o.groups[i]}, startedAt: o.startedAt}, nil
		}
	}

	return StartedOperation{}, fmt.Errorf("select started operation owner: delivery %d is absent", deliveryID)
}

func (o StartedOperation) TouchedOutboxIDs() []int64 {
	ids := make([]int64, 0, len(o.groups))
	for i := range o.groups {
		ids = append(ids, o.groups[i].ownerAfter.OutboxID)
		for j := range o.groups[i].followers {
			ids = append(ids, o.groups[i].followers[j].OutboxID)
		}
	}

	return uniqueSortedInt64s(ids)
}

func cloneTimePtr(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}

	cloned := value.UTC()

	return &cloned
}

func uniqueSortedInt64s(values []int64) []int64 {
	if len(values) == 0 {
		return nil
	}

	result := slices.Clone(values)
	slices.Sort(result)

	result = slices.Compact(result)

	return result
}

// AttemptCount는 묶음 owner 중 가장 많이 소비한 재시도 횟수다.
func (o StartedOperation) AttemptCount() int {
	count := 0

	for i := range o.groups {
		count = max(count, o.groups[i].ownerAfter.AttemptCount)
	}

	return count
}
