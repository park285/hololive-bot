package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/kapu/hololive-alarm-worker/internal/egress/sendfence"
	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/lifecycle"
	"github.com/kapu/hololive-shared/pkg/dbx"
)

// handoffFailure는 Iris failed·결과 불명 정산 하나의 전이 방식이다. 필드 status는 논리 그룹 SENT delivery의 다음 상태,
// due는 그 next_attempt_at, reissue는 요청 세대를 올리는지, quarantine은 지운 SENT 원장을 QUARANTINED 원장으로
// 바꾸는지다.
type handoffFailure struct {
	action     string
	status     lifecycle.DeliveryStatus
	due        lifecycle.DueSpec
	reissue    bool
	quarantine bool
}

// handoffSettlement는 commit 직전까지 정산 트랜잭션이 정한 결과다. 같은 트랜잭션에서 집계를 다시 계산한 부모 outbox가
// touched이고, 멤버·원장·집계를 바꾸지 않고 확인만 닫았으면 그 원인이 closed다.
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

// settleHandoffFailure는 재발급·소진·격리를 요청 하나의 트랜잭션으로 적용한다. 정산 commit 응답을 잃으면 고정 요청을
// primary에서 다시 읽어, claim 토큰이 그대로면 commit되지 않은 것(pre)이라 한 번 다시 적용하고, 기대한 정산 결과면
// 적용된 것(post)으로 본다. 요청 갱신은 delivery·원장 전이와 부모 outbox 집계와 같은 트랜잭션이므로 요청의
// post-state가 전체 commit의 증거다.
func (s *TransitionStore) settleHandoffFailure(ctx context.Context, claim *HandoffClaim, failure handoffFailure, errorText string) (ApplyResult, error) {
	if err := claim.validate(); err != nil {
		return newApplyResult(ApplyConflict, nil), fmt.Errorf("%s: %w", failure.action, err)
	}

	var prior CommitAdjudication

	for attempt := 0; attempt <= transitionOperationRetryLimit; attempt++ {
		var settlement handoffSettlement

		err := s.executeTx(ctx, failure.action, func(tx dbx.Querier) error {
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

// settleHandoffFailureTx는 요청 → 영향 부모 outbox(id 순서) → 논리 그룹 delivery → 원장 순서로 잠근다. 보존 정리·fanout과
// 같은 부모 → 자식 순서라 부모를 기다리는 동안 자식 잠금을 쥐지 않는다. 요청의 claim 소유를 잃었으면 부모·delivery를
// 잠그기 전에 미스로 끝낸다. 사전 조회와 그룹 잠금 사이에 그룹의 부모 집합이 늘었으면 새 부모를 더 잠그지 않고 아무것도
// 바꾸지 않은 membership 미스로 끝내며 확인을 닫지 않으므로, claim이 미룬 soft claim 뒤 다음 확인이 늘어난 집합으로 다시
// 정산한다. 고정 멤버나 그 논리 단위가 admission 때와 다르면 멤버·원장을 바꾸지 않고 확인을 영구히 닫으며, 잠근
// delivery만으로 어긋남이 확정되면 원장은 잠그지도 않는다. 전이를 적용했으면 마지막으로 이미 잠근 부모 중 바뀐 delivery의
// 부모 집계를 canonical 집계 문장으로 다시 계산하므로, 집계가 실패하면 delivery·원장·요청 갱신이 함께 되돌아가고 claim
// 토큰이 남아 soft claim 뒤 다음 확인이 다시 정산한다.
func (s *TransitionStore) settleHandoffFailureTx(
	ctx context.Context,
	tx dbx.Querier,
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

// applyHandoffFailure는 fence가 맞은 논리 단위에 failure를 적용하고 집계를 다시 계산한 부모 outbox를 돌려준다. 순서는
// delivery 전이 → owner를 출처로 한 SENT 원장 삭제 → (격리면) QUARANTINED 원장 기록 → 요청 재발급 또는 확인 종료 →
// 바뀐 delivery의 부모 집계이며 모두 호출자의 트랜잭션 하나다.
func applyHandoffFailure(
	ctx context.Context,
	tx dbx.Querier,
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

// handoffFailureTransitions는 각 논리 단위의 SENT delivery 전원(owner와 고정 멤버 밖 follower)을 failure.status로
// 옮긴다. 시도는 owner만 하나 쓰고 follower는 시도 수를 유지하며, 모두 같은 due를 쓴다(lifecycle.EvaluateFailure와 같은
// 규칙). PENDING·FAILED 등 SENT가 아닌 행은 이 발송의 결과가 아니므로 두고, post 단위 tracking·알람 발송 표시는 다른
// 방 발송의 결과일 수 있으므로 되돌리지 않는다.
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

// deleteHandoffSentLedger는 각 논리 키에서 그 owner를 출처로 한 SENT 원장만 지운다. 앞의 잠금에서 확인한 행이므로
// 지운 수가 단위 수와 다르면 오류다.
func deleteHandoffSentLedger(ctx context.Context, tx dbx.Querier, units []handoffUnit) error {
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

	tag, err := tx.Exec(ctx, mustSQL("handoff_ledger_delete_sent.sql"), kinds, logicalIDs, roomIDs, sources)
	if err != nil {
		return fmt.Errorf("delete sent ledger: %w", err)
	}

	if tag.RowsAffected() != int64(len(units)) {
		return fmt.Errorf("delete sent ledger: deleted %d rows, want %d", tag.RowsAffected(), len(units))
	}

	return nil
}

// recordHandoffQuarantinedLedger는 지운 SENT 원장 자리에 같은 owner를 출처로 한 QUARANTINED 원장을 전이 시각(DB 시계)으로
// 기록한다.
func recordHandoffQuarantinedLedger(ctx context.Context, tx dbx.Querier, units []handoffUnit, transitionedAt time.Time) error {
	writes := make([]LedgerWrite, 0, len(units))
	for i := range units {
		writes = append(writes, LedgerWrite{Key: units[i].key, ObservedAt: transitionedAt, SourceDeliveryID: units[i].owner.ID})
	}

	if err := RecordDeliveryLedgerWrites(ctx, tx, LedgerStatusQuarantined, writes); err != nil {
		return fmt.Errorf("record quarantined ledger: %w", err)
	}

	return nil
}

func reissueHandoffRequest(ctx context.Context, tx dbx.Querier, claim *HandoffClaim) error {
	tag, err := tx.Exec(ctx, mustSQL("handoff_request_reissue.sql"), claim.RequestBaseID, claim.Generation)
	if err != nil {
		return fmt.Errorf("reissue frozen request: %w", err)
	}

	if tag.RowsAffected() != 1 {
		return fmt.Errorf("reissue frozen request: updated %d requests, want 1", tag.RowsAffected())
	}

	return nil
}

func closeHandoffRequest(ctx context.Context, tx dbx.Querier, baseID string) error {
	tag, err := tx.Exec(ctx, mustSQL("handoff_request_close.sql"), baseID)
	if err != nil {
		return fmt.Errorf("close handoff check: %w", err)
	}

	if tag.RowsAffected() != 1 {
		return fmt.Errorf("close handoff check: updated %d requests, want 1", tag.RowsAffected())
	}

	return nil
}
