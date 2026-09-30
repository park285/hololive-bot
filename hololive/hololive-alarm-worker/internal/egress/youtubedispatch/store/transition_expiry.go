package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/lifecycle"
	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/preparation"
	"github.com/kapu/hololive-shared/pkg/dbx"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/deliverysql"
)

const expiredPendingReason = "delivery freshness expired"

type ExpiryResult struct {
	ApplyResult

	Examined int
	NextID   int64
	Expired  int
	Blocked  []BlockedLogicalGroup
}

// ExpirePending은 claim 신선도를 지난 미발송 행을 기존 논리 증거와 fence로 종료한다.
// Keyset cursor로 격리 대기 중인 오래된 그룹이 뒤의 만료 행을 가로막지 않는다.
func (s *TransitionStore) ExpirePending(ctx context.Context, afterID int64, limit int) (ExpiryResult, error) {
	if limit <= 0 || afterID < 0 {
		return ExpiryResult{}, errors.New("expire pending: invalid scan bounds")
	}

	at := time.Now().UTC().Truncate(time.Microsecond)
	result := ExpiryResult{ApplyResult: newApplyResult(ApplyApplied, nil)}

	err := s.executeTx(ctx, "expire pending", func(tx dbx.Querier) error {
		var candidates []transitionRow

		if err := deliverysql.SelectDeliverySQL(ctx, tx, &candidates, "load expired pending", mustSQL("transition_expired_pending.sql"), at.Add(-s.config.ClaimFreshnessWindow), at.Add(-s.config.LockTimeout), afterID, limit); err != nil {
			return fmt.Errorf("expire pending: candidates: %w", err)
		}

		result.Examined = len(candidates)
		if len(candidates) == 0 {
			return nil
		}

		result.NextID = candidates[len(candidates)-1].ID

		resolved, err := s.resolveStaleSendingGroups(ctx, tx, candidates, at)
		if err != nil {
			return fmt.Errorf("expire pending: resolve logical groups: %w", err)
		}

		var changes []rowTransition

		for i := range resolved.resolutions {
			resolution := &resolved.resolutions[i]
			if resolution.Kind() == preparation.LogicalInvariantBreach {
				result.Blocked = append(result.Blocked, BlockedLogicalGroup{KeyHash: resolution.Key().Hash(), Reason: resolution.InvariantReason()})
				continue
			}

			next, resolutionErr := s.expiredResolution(*resolution, resolved, at)
			if resolutionErr != nil {
				return fmt.Errorf("expire pending: resolution: %w", resolutionErr)
			}

			changes = append(changes, next...)
		}

		sortTransitionsByID(changes)

		result.TouchedOutboxIDs, err = applyRowTransitions(ctx, tx, "expire pending", changes)
		if err != nil {
			return fmt.Errorf("expire pending: apply rows: %w", err)
		}

		result.Expired = len(changes)

		return nil
	})
	if err != nil {
		result.Outcome = ApplyIndeterminate
		return result, fmt.Errorf("expire pending: transaction: %w", err)
	}

	return result, nil
}

func (s *TransitionStore) expiredResolution(resolution preparation.Resolution, resolved resolvedLogicalGroups, at time.Time) ([]rowTransition, error) {
	switch resolution.Kind() {
	case preparation.LogicalFulfilled:
		prepared := preparedClaims{}
		if err := prepareFulfilledResolution(&prepared, resolution, resolved); err != nil {
			return nil, err
		}

		return prepared.transitions, nil
	case preparation.LogicalUnresolved:
		return unresolvedTransitions(resolution, resolved.rowsByID)
	case preparation.LogicalInFlight, preparation.LogicalInvariantBreach:
		return nil, nil
	case preparation.LogicalActive, preparation.LogicalOwnerPendingElsewhere, preparation.LogicalFailed:
		owner, err := transitionRowFromSnapshot(resolution.Owner(), resolved.rowsByID)
		if err != nil {
			return nil, err
		}

		if !owner.OutboxCreatedAt.Before(at.Add(-s.config.ClaimFreshnessWindow)) {
			return nil, nil
		}

		members := resolution.Members()
		for i := range members {
			row := resolved.rowsByID[members[i].DeliveryID]
			if activeLogicalGroupLock(row, at, s.config.LockTimeout) {
				return nil, nil
			}
		}

		var changes []rowTransition

		for i := range members {
			row := resolved.rowsByID[members[i].DeliveryID]
			if row.Status != lifecycle.StatusPending {
				continue
			}

			change := failedFollowerTransition(row)

			change.after.Error = expiredPendingReason
			changes = append(changes, change)
		}

		return changes, nil
	}

	return nil, nil
}
