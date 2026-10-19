package store

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/kapu/hololive-alarm-worker/internal/egress/sendfence"
	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/lifecycle"
	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/preparation"
	"github.com/kapu/hololive-shared/pkg/dbx"
	"github.com/kapu/hololive-shared/pkg/domain"
)

const (
	fulfilledReconciliationError = "logical delivery already fulfilled"
	unresolvedPropagationError   = "logical delivery quarantined"
	failedPropagationError       = "logical delivery owner failed"
)

func (s *TransitionStore) PrepareClaimed(
	ctx context.Context,
	claimed []domain.YouTubeNotificationDelivery,
	outboxByID map[int64]domain.YouTubeNotificationOutbox,
) (PrepareClaimsResult, error) {
	if len(claimed) == 0 {
		return PrepareClaimsResult{}, nil
	}

	var prepared preparedClaims

	err := s.executeTx(ctx, "prepare claimed", func(tx dbx.Querier) error {
		var prepareErr error

		prepared, prepareErr = s.prepareClaimedTx(ctx, tx, claimed, outboxByID)
		if prepareErr != nil {
			return fmt.Errorf("prepare claimed: transaction body: %w", prepareErr)
		}

		return nil
	})
	if err != nil {
		return PrepareClaimsResult{}, fmt.Errorf("prepare claimed: execute transaction: %w", err)
	}

	return PrepareClaimsResult{
		ActiveRows:       activeClaimedRows(claimed, prepared.activeIDs),
		TouchedOutboxIDs: uniqueSortedInt64s(prepared.touched),
		Blocked:          prepared.blocked, Resolutions: prepared.kinds,
	}, nil
}

type preparedClaims struct {
	activeIDs   []int64
	touched     []int64
	blocked     []BlockedLogicalGroup
	kinds       []preparation.ResolutionKind
	transitions []rowTransition
	currentIDs  map[int64]struct{}
}

func (s *TransitionStore) prepareClaimedTx(
	ctx context.Context,
	tx dbx.Querier,
	claimed []domain.YouTubeNotificationDelivery,
	outboxByID map[int64]domain.YouTubeNotificationOutbox,
) (preparedClaims, error) {
	resolved, err := s.resolveClaimedGroups(ctx, tx, "prepare claimed", claimed, outboxByID)
	if err != nil {
		return preparedClaims{}, fmt.Errorf("prepare claimed: resolve groups: %w", err)
	}

	prepared := preparedClaims{
		transitions: make([]rowTransition, 0, len(claimed)), currentIDs: deliveryIDSet(claimed),
	}

	for i := range resolved.resolutions {
		if resolutionErr := s.prepareResolution(&prepared, resolved.resolutions[i], resolved); resolutionErr != nil {
			return preparedClaims{}, fmt.Errorf("prepare claimed: resolution %d: %w", i, resolutionErr)
		}
	}

	prepared.touched, _, err = applyRowTransitions(ctx, tx, "prepare claimed", prepared.transitions)
	if err != nil {
		return preparedClaims{}, fmt.Errorf("prepare claimed: apply resolution: %w", err)
	}

	return prepared, nil
}

func (s *TransitionStore) prepareResolution(
	prepared *preparedClaims,
	resolution preparation.Resolution,
	resolved resolvedLogicalGroups,
) error {
	prepared.kinds = append(prepared.kinds, resolution.Kind())

	var err error

	switch resolution.Kind() {
	case preparation.LogicalActive:
		err = s.prepareActiveResolution(prepared, resolution, resolved.rowsByID)
	case preparation.LogicalFulfilled:
		err = prepareFulfilledResolution(prepared, resolution, resolved)
	case preparation.LogicalUnresolved:
		err = prepareUnresolvedResolution(prepared, resolution, resolved)
	case preparation.LogicalInFlight, preparation.LogicalOwnerPendingElsewhere:
		err = s.prepareDeferredResolution(prepared, resolution, resolved.rowsByID)
	case preparation.LogicalFailed:
		err = prepareFailedResolution(prepared, resolution, resolved.rowsByID)
	case preparation.LogicalInvariantBreach:
		prepared.blocked = append(prepared.blocked, BlockedLogicalGroup{
			KeyHash: resolution.Key().Hash(), Reason: resolution.InvariantReason(),
		})

		return nil
	default:
		return fmt.Errorf("prepare claimed: unsupported resolution kind %d", resolution.Kind())
	}

	if err != nil {
		return fmt.Errorf("prepare claimed: apply %s resolution: %w", resolution.Kind(), err)
	}

	return nil
}

// prepareActiveResolution은 claim batch의 active follower를 owner 뒤로 미루며, 보류 시각은 DB 시계 + RetryBackoff이고
// owner의 next_attempt_at보다 이르지 않다.
func (s *TransitionStore) prepareActiveResolution(
	prepared *preparedClaims,
	resolution preparation.Resolution,
	rowsByID map[int64]transitionRow,
) error {
	owner := resolution.Owner()
	if _, current := prepared.currentIDs[owner.DeliveryID]; !current {
		return &transitionConflictError{
			operation: "prepare claimed", detail: fmt.Sprintf("logical owner %d is not in the claimed batch", owner.DeliveryID),
		}
	}

	prepared.activeIDs = append(prepared.activeIDs, owner.DeliveryID)

	due := lifecycle.DelayedDue(s.config.RetryBackoff, owner.NextAttemptAt)

	transitions, err := currentDeferredTransitions(resolution.Followers(), rowsByID, prepared.currentIDs, due)
	if err != nil {
		return fmt.Errorf("prepare claimed: active follower: %w", err)
	}

	prepared.transitions = append(prepared.transitions, transitions...)

	return nil
}

func prepareFulfilledResolution(
	prepared *preparedClaims,
	resolution preparation.Resolution,
	resolved resolvedLogicalGroups,
) error {
	ledger, ok := resolved.ledgerByKey[resolution.Key()]
	if !ok || ledger.Status != LedgerStatusSent || ledger.SentAt == nil {
		return &AtomicityBreachError{
			Operation: "reconcile fulfilled", Detail: "resolver returned fulfilled without SENT ledger evidence",
		}
	}

	transitions, err := fulfilledTransitions(resolution, resolved.rowsByID, *ledger.SentAt)
	if err != nil {
		return fmt.Errorf("prepare claimed: reconcile fulfilled: %w", err)
	}

	prepared.transitions = append(prepared.transitions, transitions...)

	return nil
}

func prepareUnresolvedResolution(
	prepared *preparedClaims,
	resolution preparation.Resolution,
	resolved resolvedLogicalGroups,
) error {
	ledger, ok := resolved.ledgerByKey[resolution.Key()]
	if !ok || ledger.Status != LedgerStatusQuarantined {
		return &AtomicityBreachError{
			Operation: "propagate unresolved", Detail: "resolver returned unresolved without QUARANTINED ledger evidence",
		}
	}

	transitions, err := unresolvedTransitions(resolution, resolved.rowsByID)
	if err != nil {
		return fmt.Errorf("prepare claimed: propagate unresolved: %w", err)
	}

	prepared.transitions = append(prepared.transitions, transitions...)

	return nil
}

// prepareDeferredResolution은 in-flight·다른 batch owner 그룹의 claim 행을 DB 시계 + RetryBackoff 뒤로 미루며,
// resolver가 준 DB 유래 하한(owner due 또는 in-flight lease 만료)보다 이르지 않게 한다.
func (s *TransitionStore) prepareDeferredResolution(
	prepared *preparedClaims,
	resolution preparation.Resolution,
	rowsByID map[int64]transitionRow,
) error {
	due := lifecycle.DelayedDue(s.config.RetryBackoff, resolution.DueFloor())

	transitions, err := currentDeferredTransitions(resolution.Members(), rowsByID, prepared.currentIDs, due)
	if err != nil {
		return fmt.Errorf("prepare claimed: defer member: %w", err)
	}

	prepared.transitions = append(prepared.transitions, transitions...)

	return nil
}

func prepareFailedResolution(
	prepared *preparedClaims,
	resolution preparation.Resolution,
	rowsByID map[int64]transitionRow,
) error {
	members := resolution.Members()
	for i := range members {
		member := members[i]
		if _, current := prepared.currentIDs[member.DeliveryID]; !current {
			continue
		}

		row, err := transitionRowFromSnapshot(member, rowsByID)
		if err != nil {
			return fmt.Errorf("prepare claimed: failed member: %w", err)
		}

		prepared.transitions = append(prepared.transitions, failedFollowerTransition(row))
	}

	return nil
}

func currentDeferredTransitions(
	members []preparation.DeliverySnapshot,
	rowsByID map[int64]transitionRow,
	currentIDs map[int64]struct{},
	due lifecycle.DueSpec,
) ([]rowTransition, error) {
	transitions := make([]rowTransition, 0, len(members))
	for i := range members {
		member := members[i]
		if _, current := currentIDs[member.DeliveryID]; !current {
			continue
		}

		row, err := transitionRowFromSnapshot(member, rowsByID)
		if err != nil {
			return nil, fmt.Errorf("defer claimed member: %w", err)
		}

		transitions = append(transitions, deferTransition(row, due))
	}

	return transitions, nil
}

func activeClaimedRows(
	claimed []domain.YouTubeNotificationDelivery,
	activeIDs []int64,
) []domain.YouTubeNotificationDelivery {
	activeSet := make(map[int64]struct{}, len(activeIDs))
	for _, id := range activeIDs {
		activeSet[id] = struct{}{}
	}

	active := make([]domain.YouTubeNotificationDelivery, 0, len(activeIDs))

	for i := range claimed {
		if _, ok := activeSet[claimed[i].ID]; ok {
			active = append(active, claimed[i])
		}
	}

	return active
}

func deferTransition(row transitionRow, due lifecycle.DueSpec) rowTransition {
	after := row

	after.Status = lifecycle.StatusPending
	after.RowVersion++

	after.LockedAt = nil
	after.LockedBy = nil
	after.Error = ""
	after.SentAt = nil

	return rowTransition{before: row, after: after, due: due}
}

func fulfilledTransitions(
	resolution preparation.Resolution,
	rowsByID map[int64]transitionRow,
	sentAt time.Time,
) ([]rowTransition, error) {
	canonicalSentAt, err := lifecycle.CanonicalTime(sentAt)
	if err != nil {
		return nil, fmt.Errorf("fulfilled transitions: sent at: %w", err)
	}

	transitions := make([]rowTransition, 0, len(resolution.Members()))
	members := resolution.Members()

	for i := range members {
		member := members[i]
		if member.Status == lifecycle.StatusSent {
			continue
		}

		row, rowErr := transitionRowFromSnapshot(member, rowsByID)
		if rowErr != nil {
			return nil, fmt.Errorf("fulfilled transitions: %w", rowErr)
		}

		after := row

		after.Status = lifecycle.StatusSent
		after.RowVersion++

		after.LockedAt = nil
		after.LockedBy = nil
		after.SentAt = &canonicalSentAt
		after.Error = ""
		transitions = append(transitions, rowTransition{before: row, after: after})
	}

	return transitions, nil
}

func unresolvedTransitions(
	resolution preparation.Resolution,
	rowsByID map[int64]transitionRow,
) ([]rowTransition, error) {
	transitions := make([]rowTransition, 0, len(resolution.Members()))
	members := resolution.Members()

	for i := range members {
		member := members[i]
		if member.Status == lifecycle.StatusQuarantined {
			continue
		}

		if member.Status == lifecycle.StatusSent || member.Status == lifecycle.StatusSending {
			return nil, &AtomicityBreachError{
				Operation: "propagate unresolved",
				Detail:    fmt.Sprintf("delivery %d has incompatible status %s", member.DeliveryID, member.Status),
			}
		}

		row, err := transitionRowFromSnapshot(member, rowsByID)
		if err != nil {
			return nil, fmt.Errorf("unresolved transitions: %w", err)
		}

		after := row

		after.Status = lifecycle.StatusQuarantined
		after.RowVersion++

		after.LockedAt = nil
		after.LockedBy = nil
		after.SentAt = nil
		after.Error = unresolvedPropagationError
		transitions = append(transitions, rowTransition{before: row, after: after})
	}

	return transitions, nil
}

func failedFollowerTransition(row transitionRow) rowTransition {
	after := row

	after.Status = lifecycle.StatusFailed
	after.RowVersion++

	after.LockedAt = nil
	after.LockedBy = nil
	after.SentAt = nil
	after.Error = failedPropagationError

	return rowTransition{before: row, after: after}
}

// DeferCommand는 claim한 follower 한 행을 DB 시계에서 Delay 뒤로 미룬다.
type DeferCommand struct {
	Delivery domain.YouTubeNotificationDelivery
	Delay    time.Duration
}

func (s *TransitionStore) DeferFollower(ctx context.Context, command DeferCommand) (ApplyResult, error) {
	if command.Delivery.ID <= 0 || command.Delivery.LockedAt == nil || command.Delivery.RowVersion <= 0 {
		return newApplyResult(ApplyConflict, nil), errors.New("defer follower: invalid preparation fence")
	}

	if command.Delay < 0 {
		return newApplyResult(ApplyConflict, nil), errors.New("defer follower: delay is negative")
	}

	before := transitionRow{
		ID: command.Delivery.ID, OutboxID: command.Delivery.OutboxID,
		RoomID: command.Delivery.RoomID, Status: lifecycle.DeliveryStatus(command.Delivery.Status),
		AttemptCount: command.Delivery.AttemptCount, NextAttemptAt: command.Delivery.NextAttemptAt,
		CreatedAt: command.Delivery.CreatedAt, LockedAt: cloneTimePtr(command.Delivery.LockedAt),
		LockedBy: cloneStringPtr(command.Delivery.LockedBy),
		SentAt:   cloneTimePtr(command.Delivery.SentAt), Error: command.Delivery.Error,
		RowVersion: command.Delivery.RowVersion,
	}
	transition := deferTransition(before, lifecycle.DelayedDue(command.Delay, time.Time{}))

	var touched []int64

	if err := s.executeTx(ctx, "defer follower", func(tx dbx.Querier) error {
		var applyErr error

		touched, _, applyErr = applyRowTransitions(ctx, tx, "defer follower", []rowTransition{transition})
		if applyErr != nil {
			return fmt.Errorf("defer follower: apply row: %w", applyErr)
		}

		return nil
	}); err != nil {
		if miss, ok := errors.AsType[*sendfence.MissError](err); ok {
			return newApplyResult(fenceMissOutcome(miss), nil), fmt.Errorf("defer follower: %w", err)
		}

		return newApplyResult(ApplyIndeterminate, nil), fmt.Errorf("defer follower: execute transaction: %w", err)
	}

	return newApplyResult(ApplyApplied, touched), nil
}

func sortTransitionsByID(transitions []rowTransition) {
	slices.SortFunc(transitions, func(left, right rowTransition) int {
		if left.before.ID < right.before.ID {
			return -1
		}

		if left.before.ID > right.before.ID {
			return 1
		}

		return 0
	})
}
