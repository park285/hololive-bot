package store

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

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

	if err := s.ensureReady(ctx); err != nil {
		return PrepareClaimsResult{}, fmt.Errorf("prepare claimed: %w", err)
	}

	at, err := lifecycle.CanonicalTime(time.Now())
	if err != nil {
		return PrepareClaimsResult{}, fmt.Errorf("prepare claimed: at: %w", err)
	}

	var prepared preparedClaims

	err = s.executeTx(ctx, "prepare claimed", func(tx dbx.Querier) error {
		var prepareErr error

		prepared, prepareErr = s.prepareClaimedTx(ctx, tx, claimed, outboxByID, at)
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
	at time.Time,
) (preparedClaims, error) {
	resolved, err := s.resolveClaimedGroups(ctx, tx, claimed, outboxByID, at)
	if err != nil {
		return preparedClaims{}, fmt.Errorf("prepare claimed: resolve groups: %w", err)
	}

	prepared := preparedClaims{
		transitions: make([]rowTransition, 0, len(claimed)), currentIDs: deliveryIDSet(claimed),
	}

	for i := range resolved.resolutions {
		if resolutionErr := s.prepareResolution(&prepared, resolved.resolutions[i], resolved, at); resolutionErr != nil {
			return preparedClaims{}, fmt.Errorf("prepare claimed: resolution %d: %w", i, resolutionErr)
		}
	}

	prepared.touched, err = applyRowTransitions(ctx, tx, "prepare claimed", prepared.transitions)
	if err != nil {
		return preparedClaims{}, fmt.Errorf("prepare claimed: apply resolution: %w", err)
	}

	return prepared, nil
}

func (s *TransitionStore) prepareResolution(
	prepared *preparedClaims,
	resolution preparation.Resolution,
	resolved resolvedLogicalGroups,
	at time.Time,
) error {
	prepared.kinds = append(prepared.kinds, resolution.Kind())

	var err error

	switch resolution.Kind() {
	case preparation.LogicalActive:
		err = s.prepareActiveResolution(prepared, resolution, resolved.rowsByID, at)
	case preparation.LogicalFulfilled:
		err = prepareFulfilledResolution(prepared, resolution, resolved)
	case preparation.LogicalUnresolved:
		err = prepareUnresolvedResolution(prepared, resolution, resolved)
	case preparation.LogicalInFlight, preparation.LogicalOwnerPendingElsewhere:
		err = s.prepareDeferredResolution(prepared, resolution, resolved.rowsByID, at)
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

func (s *TransitionStore) prepareActiveResolution(
	prepared *preparedClaims,
	resolution preparation.Resolution,
	rowsByID map[int64]transitionRow,
	at time.Time,
) error {
	owner := resolution.Owner()
	if _, current := prepared.currentIDs[owner.DeliveryID]; !current {
		return &transitionConflictError{
			operation: "prepare claimed", detail: fmt.Sprintf("logical owner %d is not in the claimed batch", owner.DeliveryID),
		}
	}

	prepared.activeIDs = append(prepared.activeIDs, owner.DeliveryID)

	deferAt := maxCanonicalTime(at.Add(s.config.RetryBackoff), owner.NextAttemptAt)

	transitions, err := currentDeferredTransitions(resolution.Followers(), rowsByID, prepared.currentIDs, deferAt)
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

func (s *TransitionStore) prepareDeferredResolution(
	prepared *preparedClaims,
	resolution preparation.Resolution,
	rowsByID map[int64]transitionRow,
	at time.Time,
) error {
	deferAt := resolution.Due()
	if deferAt.IsZero() {
		deferAt = at.Add(s.config.RetryBackoff)
	}

	transitions, err := currentDeferredTransitions(resolution.Members(), rowsByID, prepared.currentIDs, deferAt)
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
	deferAt time.Time,
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

		transitions = append(transitions, deferTransition(row, deferAt))
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

func deferTransition(row transitionRow, due time.Time) rowTransition {
	after := row

	after.Status = lifecycle.StatusPending
	after.RowVersion++

	after.NextAttemptAt = due.UTC().Truncate(time.Microsecond)
	after.LockedAt = nil
	after.Error = ""
	after.SentAt = nil

	return rowTransition{before: row, after: after}
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
	after.SentAt = nil
	after.Error = failedPropagationError

	return rowTransition{before: row, after: after}
}

func maxCanonicalTime(left, right time.Time) time.Time {
	value := left
	if right.After(value) {
		value = right
	}

	return value.UTC().Truncate(time.Microsecond)
}

type DeferCommand struct {
	Delivery      domain.YouTubeNotificationDelivery
	NextAttemptAt time.Time
}

func (s *TransitionStore) DeferFollower(ctx context.Context, command DeferCommand) (ApplyResult, error) {
	if err := s.ensureReady(ctx); err != nil {
		return newApplyResult(ApplyIndeterminate, nil), fmt.Errorf("defer follower: %w", err)
	}

	if command.Delivery.ID <= 0 || command.Delivery.LockedAt == nil || command.Delivery.RowVersion <= 0 {
		return newApplyResult(ApplyConflict, nil), errors.New("defer follower: invalid preparation fence")
	}

	due, err := lifecycle.CanonicalTime(command.NextAttemptAt)
	if err != nil {
		return newApplyResult(ApplyConflict, nil), fmt.Errorf("defer follower: due: %w", err)
	}

	before := transitionRow{
		ID: command.Delivery.ID, OutboxID: command.Delivery.OutboxID,
		RoomID: command.Delivery.RoomID, Status: lifecycle.DeliveryStatus(command.Delivery.Status),
		AttemptCount: command.Delivery.AttemptCount, NextAttemptAt: command.Delivery.NextAttemptAt,
		CreatedAt: command.Delivery.CreatedAt, LockedAt: cloneTimePtr(command.Delivery.LockedAt),
		SentAt: cloneTimePtr(command.Delivery.SentAt), Error: command.Delivery.Error,
		RowVersion: command.Delivery.RowVersion,
	}
	transition := deferTransition(before, due)

	var touched []int64

	if err := s.executeTx(ctx, "defer follower", func(tx dbx.Querier) error {
		var applyErr error

		touched, applyErr = applyRowTransitions(ctx, tx, "defer follower", []rowTransition{transition})
		if applyErr != nil {
			return fmt.Errorf("defer follower: apply row: %w", applyErr)
		}

		return nil
	}); err != nil {
		if _, ok := errors.AsType[*transitionConflictError](err); ok {
			return newApplyResult(ApplyConflict, nil), nil
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
