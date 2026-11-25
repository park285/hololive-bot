package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/park285/shared-go/postgres/pgxdb"

	"github.com/kapu/hololive-alarm-worker/internal/egress/sendfence"
	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/lifecycle"
)

type handoffFailure struct {
	action     string
	status     lifecycle.DeliveryStatus
	due        lifecycle.DueSpec
	reissue    bool
	quarantine bool
	pauseRoom  bool
}

type handoffSettlement struct {
	touched []int64
	closed  error
}

func (h handoffSettlement) result(adjudication CommitAdjudication) (ApplyResult, error) {
	if h.closed != nil {
		result := newApplyResult(ApplyConflict, nil)

		result.CommitAdjudication = adjudication

		return result, fmt.Errorf("%w: %w", ErrHandoffCheckClosed, h.closed)
	}

	result := newApplyResult(ApplyApplied, h.touched)

	result.CommitAdjudication = adjudication

	return result, nil
}

func (s *TransitionStore) settleHandoffFailure(ctx context.Context, claim *HandoffClaim, failure handoffFailure, errorText string) (ApplyResult, error) {
	if err := claim.validate(); err != nil {
		return newApplyResult(ApplyConflict, nil), fmt.Errorf("%s: %w", failure.action, err)
	}

	var prior CommitAdjudication

	for attempt := 0; attempt <= transitionOperationRetryLimit; attempt++ {
		var settlement handoffSettlement

		err := s.executeTx(ctx, failure.action, func(tx pgxdb.Querier) error {
			var txErr error

			settlement, txErr = s.settleHandoffFailureTx(ctx, tx, claim, failure, errorText)

			return txErr
		})
		if err == nil {
			return settlement.result(prior)
		}

		if _, lost := errors.AsType[*commitResponseError](err); !lost {
			return handoffMissResult(err), fmt.Errorf("settle youtube handoff: %w", err)
		}

		state, found, readErr := loadHandoffRequestState(ctx, s.db, claim.RequestBaseID)

		switch {
		case readErr != nil:
			return adjudicated(newApplyResult(ApplyIndeterminate, nil), err, CommitIndeterminate),
				fmt.Errorf("settle youtube handoff: %w", errors.Join(err, readErr))
		case !found:
			return adjudicated(newApplyResult(ApplyMissing, nil), err, CommitMissing), fmt.Errorf("settle youtube handoff: %w", err)
		case state.heldBy(claim):
			prior = CommitConfirmedPre

			if attempt < transitionOperationRetryLimit {
				continue
			}

			result := newApplyResult(ApplyConflict, nil)

			result.CommitAdjudication = prior

			return result, fmt.Errorf("settle youtube handoff: %w", err)
		case settlement.closed != nil || !failure.reissue:
			if state.closedBy(claim) {
				return settlement.result(CommitConfirmedPost)
			}
		case state.reissuedFrom(claim):
			return settlement.result(CommitConfirmedPost)
		}

		return adjudicated(newApplyResult(ApplyIndeterminate, nil), err, CommitConflict), fmt.Errorf("settle youtube handoff: %w", err)
	}

	return newApplyResult(ApplyIndeterminate, nil), fmt.Errorf("%s: retry loop exhausted", failure.action)
}

func (s *TransitionStore) settleHandoffFailureTx(
	ctx context.Context,
	tx pgxdb.Querier,
	claim *HandoffClaim,
	failure handoffFailure,
	errorText string,
) (handoffSettlement, error) {
	fence, err := queryHandoffFence(ctx, tx, handoffRequestLockSQL, claim.fenceArgs()...)
	if err != nil {
		return handoffSettlement{}, fmt.Errorf("lock frozen request: %w", err)
	}

	if miss := fence.claimMiss(failure.action, claim); miss != nil {
		return handoffSettlement{}, miss
	}

	units, observed, err := s.lockHandoffUnits(ctx, tx, failure.action, claim)
	if err != nil {
		return handoffSettlement{}, err
	}

	if miss := sendfence.Check(failure.action, claim.MemberIDs, observed); miss != nil {
		if closeErr := closeHandoffRequest(ctx, tx, claim.RequestBaseID); closeErr != nil {
			return handoffSettlement{}, closeErr
		}

		return handoffSettlement{closed: miss}, nil
	}

	touched, err := applyHandoffFailure(ctx, tx, claim, units, failure, errorText)
	if err != nil {
		return handoffSettlement{}, err
	}

	return handoffSettlement{touched: touched}, nil
}

func applyHandoffFailure(
	ctx context.Context,
	tx pgxdb.Querier,
	claim *HandoffClaim,
	units []handoffUnit,
	failure handoffFailure,
	errorText string,
) ([]int64, error) {
	touched, transitionedAt, err := applyRowTransitions(ctx, tx, failure.action, handoffFailureTransitions(units, failure, errorText))
	if err != nil {
		return nil, fmt.Errorf("transition logical group rows: %w", err)
	}

	if deleteErr := deleteHandoffSentLedger(ctx, tx, units); deleteErr != nil {
		return nil, deleteErr
	}

	if failure.quarantine {
		if recordErr := recordHandoffQuarantinedLedger(ctx, tx, units, transitionedAt); recordErr != nil {
			return nil, recordErr
		}
	}

	if failure.pauseRoom {
		if pauseErr := pauseHandoffRooms(ctx, tx, units); pauseErr != nil {
			return nil, pauseErr
		}
	}

	if failure.reissue {
		err = reissueHandoffRequest(ctx, tx, claim)
	} else {
		err = closeHandoffRequest(ctx, tx, claim.RequestBaseID)
	}

	if err != nil {
		return nil, err
	}

	if err = syncOutboxAggregates(ctx, tx, touched); err != nil {
		return nil, fmt.Errorf("project outbox aggregates: %w", err)
	}

	return touched, nil
}

func handoffFailureTransitions(units []handoffUnit, failure handoffFailure, errorText string) []rowTransition {
	var transitions []rowTransition

	for i := range units {
		for j := range units[i].rows {
			before := &units[i].rows[j]
			if before.Status != lifecycle.StatusSent {
				continue
			}

			after := *before

			after.Status = failure.status
			after.RowVersion++

			if before.ID == units[i].owner.ID {
				after.AttemptCount++
			}

			after.LockedAt = nil
			after.LockedBy = nil
			after.SentAt = nil
			after.Error = errorText
			transitions = append(transitions, rowTransition{before: *before, after: after, due: failure.due})
		}
	}

	sortTransitionsByID(transitions)

	return transitions
}

func deleteHandoffSentLedger(ctx context.Context, tx pgxdb.Querier, units []handoffUnit) error {
	kinds := make([]string, 0, len(units))
	logicalIDs := make([]string, 0, len(units))
	roomIDs := make([]string, 0, len(units))
	sources := make([]int64, 0, len(units))

	for i := range units {
		kinds = append(kinds, string(units[i].key.Kind))
		logicalIDs = append(logicalIDs, units[i].key.LogicalID)
		roomIDs = append(roomIDs, units[i].key.RoomID)
		sources = append(sources, units[i].owner.ID)
	}

	tag, err := pgxdb.Exec(ctx, tx, handoffLedgerDeleteSentSQL, kinds, logicalIDs, roomIDs, sources)
	if err != nil {
		return fmt.Errorf("delete sent ledger: %w", err)
	}

	if tag.RowsAffected() != int64(len(units)) {
		return fmt.Errorf("delete sent ledger: deleted %d rows, want %d", tag.RowsAffected(), len(units))
	}

	return nil
}

func recordHandoffQuarantinedLedger(ctx context.Context, tx pgxdb.Querier, units []handoffUnit, transitionedAt time.Time) error {
	writes := make([]LedgerWrite, 0, len(units))
	for i := range units {
		writes = append(writes, LedgerWrite{Key: units[i].key, ObservedAt: transitionedAt, SourceDeliveryID: units[i].owner.ID})
	}

	if err := RecordDeliveryLedgerWrites(ctx, tx, LedgerStatusQuarantined, writes); err != nil {
		return fmt.Errorf("record quarantined ledger: %w", err)
	}

	return nil
}

func pauseHandoffRooms(ctx context.Context, tx pgxdb.Querier, units []handoffUnit) error {
	roomIDs := make([]string, 0, len(units))
	for i := range units {
		roomIDs = append(roomIDs, units[i].key.RoomID)
	}

	if _, err := pgxdb.Exec(ctx, tx, handoffRoomPauseSQL, roomIDs); err != nil {
		return fmt.Errorf("pause rooms after handoff: %w", err)
	}

	return nil
}

func reissueHandoffRequest(ctx context.Context, tx pgxdb.Querier, claim *HandoffClaim) error {
	tag, err := pgxdb.Exec(ctx, tx, handoffRequestReissueSQL, claim.RequestBaseID, claim.Generation)
	if err != nil {
		return fmt.Errorf("reissue frozen request: %w", err)
	}

	if tag.RowsAffected() != 1 {
		return fmt.Errorf("reissue frozen request: updated %d requests, want 1", tag.RowsAffected())
	}

	return nil
}

func closeHandoffRequest(ctx context.Context, tx pgxdb.Querier, baseID string) error {
	tag, err := pgxdb.Exec(ctx, tx, handoffRequestCloseSQL, baseID)
	if err != nil {
		return fmt.Errorf("close handoff check: %w", err)
	}

	if tag.RowsAffected() != 1 {
		return fmt.Errorf("close handoff check: updated %d requests, want 1", tag.RowsAffected())
	}

	return nil
}
