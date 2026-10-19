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
	ytcontentid "github.com/kapu/hololive-shared/pkg/service/youtube/contentid"
)

func (s *TransitionStore) BeginSending(
	ctx context.Context,
	rows []domain.YouTubeNotificationDelivery,
	outboxByID map[int64]domain.YouTubeNotificationOutbox,
) (StartedOperation, ApplyResult, error) {
	if len(rows) == 0 {
		return StartedOperation{}, newApplyResult(ApplyConflict, nil), errors.New("begin sending: rows are empty")
	}

	var priorAdjudication CommitAdjudication

	for attempt := 0; attempt <= transitionOperationRetryLimit; attempt++ {
		started, transitions, touched, applyErr := s.beginSendingOnce(ctx, rows, outboxByID)
		if applyErr == nil {
			result := newApplyResult(ApplyApplied, touched)

			result.CommitAdjudication = priorAdjudication

			return started, result, nil
		}

		decision := s.adjudicateRowApply(
			ctx,
			"begin sending",
			"owner rows are split across exact pre-state and post-state",
			transitions,
			nil,
			applyErr,
			priorAdjudication,
			attempt,
		)
		if decision.retry {
			priorAdjudication = decision.prior

			continue
		}

		if decision.result.Outcome == ApplyApplied {
			return started, decision.result, decision.err
		}

		return StartedOperation{}, decision.result, decision.err
	}

	return StartedOperation{}, newApplyResult(ApplyIndeterminate, nil), errors.New("begin sending: retry loop exhausted")
}

func (s *TransitionStore) beginSendingOnce(
	ctx context.Context,
	rows []domain.YouTubeNotificationDelivery,
	outboxByID map[int64]domain.YouTubeNotificationOutbox,
) (StartedOperation, []rowTransition, []int64, error) {
	var (
		started     StartedOperation
		transitions []rowTransition
		touched     []int64
	)

	err := s.executeTx(ctx, "begin sending", func(tx dbx.Querier) error {
		var txErr error

		started, transitions, touched, txErr = s.beginSendingTx(ctx, tx, rows, outboxByID)
		if txErr != nil {
			return fmt.Errorf("begin sending: transaction body: %w", txErr)
		}

		return nil
	})
	if err != nil {
		return started, transitions, touched, fmt.Errorf("begin sending: execute transaction: %w", err)
	}

	return started, transitions, touched, nil
}

// beginSendingTx는 owner를 SENDING으로 전이하고, 전이 문장이 잠근 뒤 채취한 DB 시계를 owner의 locked_at(발송 시작
// 시각)으로 StartedOperation에 싣는다.
func (s *TransitionStore) beginSendingTx(
	ctx context.Context,
	tx dbx.Querier,
	rows []domain.YouTubeNotificationDelivery,
	outboxByID map[int64]domain.YouTubeNotificationOutbox,
) (StartedOperation, []rowTransition, []int64, error) {
	resolved, err := s.resolveClaimedGroups(ctx, tx, "begin sending", rows, outboxByID)
	if err != nil {
		return StartedOperation{}, nil, nil, fmt.Errorf("begin sending: resolve groups: %w", err)
	}

	requestedIDs := deliveryIDSet(rows)

	groups, transitions, err := buildStartedGroups(resolved, requestedIDs)
	if err != nil {
		return StartedOperation{}, transitions, nil, fmt.Errorf("begin sending: build groups: %w", err)
	}

	if len(groups) != len(requestedIDs) {
		return StartedOperation{}, transitions, nil, &transitionConflictError{
			operation: "begin sending",
			detail:    fmt.Sprintf("resolved owner count %d does not match requested count %d", len(groups), len(requestedIDs)),
		}
	}

	sortTransitionsByID(transitions)

	touched, startedAt, err := applySendingTransitions(ctx, tx, "begin sending", transitions, s.config.LockTimeout)
	if err != nil {
		return StartedOperation{}, transitions, nil, fmt.Errorf("begin sending: apply rows: %w", err)
	}

	for i := range groups {
		groups[i].ownerAfter.LockedAt = new(startedAt)
	}

	return StartedOperation{groups: groups}, transitions, touched, nil
}

func deliveryIDSet(rows []domain.YouTubeNotificationDelivery) map[int64]struct{} {
	ids := make(map[int64]struct{}, len(rows))
	for i := range rows {
		ids[rows[i].ID] = struct{}{}
	}

	return ids
}

func buildStartedGroups(
	resolved resolvedLogicalGroups,
	requestedIDs map[int64]struct{},
) ([]startedLogicalGroup, []rowTransition, error) {
	groups := make([]startedLogicalGroup, 0, len(resolved.resolutions))
	transitions := make([]rowTransition, 0, len(resolved.resolutions))

	for i := range resolved.resolutions {
		group, transition, err := buildStartedGroup(resolved.resolutions[i], resolved, requestedIDs)
		if err != nil {
			return nil, transitions, fmt.Errorf("begin sending: build logical group: %w", err)
		}

		groups = append(groups, group)
		transitions = append(transitions, transition)
	}

	return groups, transitions, nil
}

func buildStartedGroup(
	resolution preparation.Resolution,
	resolved resolvedLogicalGroups,
	requestedIDs map[int64]struct{},
) (startedLogicalGroup, rowTransition, error) {
	if resolution.Kind() != preparation.LogicalActive {
		return startedLogicalGroup{}, rowTransition{}, &transitionConflictError{
			operation: "begin sending",
			detail:    fmt.Sprintf("logical group %s resolution is %d", resolution.Key().Hash(), resolution.Kind()),
		}
	}

	owner := resolution.Owner()
	if _, requested := requestedIDs[owner.DeliveryID]; !requested {
		return startedLogicalGroup{}, rowTransition{}, &transitionConflictError{
			operation: "begin sending", detail: fmt.Sprintf("deterministic owner %d is not requested", owner.DeliveryID),
		}
	}

	before, err := transitionRowFromSnapshot(owner, resolved.rowsByID)
	if err != nil {
		return startedLogicalGroup{}, rowTransition{}, fmt.Errorf("begin sending: owner: %w", err)
	}

	followers, err := startedFollowers(resolution, resolved.rowsByID)
	if err != nil {
		return startedLogicalGroup{}, rowTransition{}, fmt.Errorf("begin sending: load followers: %w", err)
	}

	after := sendingTransitionRow(before)
	group := startedLogicalGroup{
		key: resolution.Key(), ownerBefore: before, ownerAfter: after,
		followers: followers, ledgerBefore: ledgerRecordPointer(resolved.ledgerByKey, resolution), providerOwner: true,
	}

	return group, rowTransition{before: before, after: after, lockFromClock: true}, nil
}

func startedFollowers(
	resolution preparation.Resolution,
	rowsByID map[int64]transitionRow,
) ([]transitionRow, error) {
	followers := make([]transitionRow, 0, len(resolution.Followers()))
	followerSnapshots := resolution.Followers()

	for i := range followerSnapshots {
		row, err := transitionRowFromSnapshot(followerSnapshots[i], rowsByID)
		if err != nil {
			return nil, fmt.Errorf("begin sending: follower: %w", err)
		}

		followers = append(followers, row)
	}

	return followers, nil
}

// sendingTransitionRow는 claim 소유자(locked_by)를 유지한 채 SENDING으로 올린다. 발송 시작 시각(locked_at)은 전이
// 문장의 DB 시계로 기록한다.
func sendingTransitionRow(before transitionRow) transitionRow {
	after := before

	after.Status = lifecycle.StatusSending
	after.RowVersion++

	after.LockedAt = nil
	after.SentAt = nil
	after.Error = ""

	return after
}

func ledgerRecordPointer(
	ledgerByKey map[ytcontentid.LogicalKey]DeliveryLedgerRecord,
	resolution preparation.Resolution,
) *DeliveryLedgerRecord {
	ledger, ok := ledgerByKey[resolution.Key()]
	if !ok {
		return nil
	}

	return &ledger
}

type envelopeState uint8

const (
	envelopePre envelopeState = iota + 1
	envelopePost
	envelopeConflict
	envelopeMissing
	envelopeMixed
)

func (s *TransitionStore) classifyRowEnvelope(
	ctx context.Context,
	operation string,
	transitions []rowTransition,
) (envelopeState, error) {
	ids := make([]int64, 0, len(transitions))
	for i := range transitions {
		ids = append(ids, transitions[i].before.ID)
	}

	var rows []transitionRow

	if err := deliverysqlSelectTransitionRows(ctx, s.db, ids, &rows); err != nil {
		return 0, fmt.Errorf("%s: primary read-back: %w", operation, err)
	}

	if len(rows) != len(ids) {
		return envelopeMissing, nil
	}

	byID := make(map[int64]transitionRow, len(rows))
	for i := range rows {
		byID[rows[i].ID] = rows[i]
	}

	preCount := 0
	postCount := 0

	for i := range transitions {
		actual, ok := byID[transitions[i].before.ID]
		if !ok {
			return envelopeMissing, nil
		}

		if sameTransitionState(actual, transitions[i].after) {
			postCount++
			continue
		}

		if sameTransitionState(actual, transitions[i].before) {
			preCount++
		}
	}

	if postCount == len(transitions) {
		return envelopePost, nil
	}

	if preCount == len(transitions) {
		return envelopePre, nil
	}

	if preCount > 0 || postCount > 0 {
		return envelopeMixed, nil
	}

	return envelopeConflict, nil
}

func deliverysqlSelectTransitionRows(
	ctx context.Context,
	db dbx.Querier,
	ids []int64,
	dest *[]transitionRow,
) error {
	if err := dbx.SelectSQL(
		ctx,
		db,
		dest,
		"read transition rows",
		mustSQL("transition_read_rows.sql"),
		uniqueSortedInt64s(ids),
	); err != nil {
		return fmt.Errorf("read transition rows: %w", err)
	}

	return nil
}

// sameTransitionState는 commit 판정용 상태 등가다. 모든 쓰기가 row_version을 올리므로 시각 열(next_attempt_at·
// locked_at·sent_at)은 비교하지 않으며, 이 값들은 전이 문장의 DB 시계가 정해 호출자가 미리 알 수 없다.
func sameTransitionState(actual, expected transitionRow) bool {
	return actual.ID == expected.ID &&
		actual.OutboxID == expected.OutboxID &&
		actual.Status == expected.Status &&
		actual.AttemptCount == expected.AttemptCount &&
		actual.RowVersion == expected.RowVersion &&
		sameOwner(actual.LockedBy, expected.LockedBy) &&
		actual.Error == expected.Error
}

func timesEqual(left, right *time.Time) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}

	return left.Equal(*right)
}

func transitionOutboxIDs(transitions []rowTransition) []int64 {
	ids := make([]int64, 0, len(transitions))
	for i := range transitions {
		ids = append(ids, transitions[i].after.OutboxID)
	}

	return uniqueSortedInt64s(ids)
}

func sortedStartedGroups(groups []startedLogicalGroup) []startedLogicalGroup {
	result := slices.Clone(groups)
	slices.SortFunc(result, func(left, right startedLogicalGroup) int {
		if left.ownerAfter.ID < right.ownerAfter.ID {
			return -1
		}

		if left.ownerAfter.ID > right.ownerAfter.ID {
			return 1
		}

		return 0
	})

	return result
}
