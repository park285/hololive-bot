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
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/deliverysql"
)

func (s *TransitionStore) BeginSending(
	ctx context.Context,
	rows []domain.YouTubeNotificationDelivery,
	outboxByID map[int64]domain.YouTubeNotificationOutbox,
) (StartedOperation, ApplyResult, error) {
	if len(rows) == 0 {
		return StartedOperation{}, newApplyResult(ApplyConflict, nil), errors.New("begin sending: rows are empty")
	}

	if err := s.ensureReady(ctx); err != nil {
		return StartedOperation{}, newApplyResult(ApplyIndeterminate, nil), fmt.Errorf("begin sending: %w", err)
	}

	startedAt, err := lifecycle.CanonicalTime(time.Now())
	if err != nil {
		return StartedOperation{}, newApplyResult(ApplyIndeterminate, nil), fmt.Errorf("begin sending: started at: %w", err)
	}

	var priorAdjudication CommitAdjudication

	for attempt := 0; attempt <= transitionOperationRetryLimit; attempt++ {
		started, transitions, touched, applyErr := s.beginSendingOnce(ctx, rows, outboxByID, startedAt)
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
	startedAt time.Time,
) (StartedOperation, []rowTransition, []int64, error) {
	var (
		started     StartedOperation
		transitions []rowTransition
		touched     []int64
	)

	err := s.executeTx(ctx, "begin sending", func(tx dbx.Querier) error {
		var txErr error

		started, transitions, touched, txErr = s.beginSendingTx(ctx, tx, rows, outboxByID, startedAt)
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

func (s *TransitionStore) beginSendingTx(
	ctx context.Context,
	tx dbx.Querier,
	rows []domain.YouTubeNotificationDelivery,
	outboxByID map[int64]domain.YouTubeNotificationOutbox,
	startedAt time.Time,
) (StartedOperation, []rowTransition, []int64, error) {
	resolved, err := s.resolveClaimedGroups(ctx, tx, rows, outboxByID, startedAt)
	if err != nil {
		return StartedOperation{}, nil, nil, fmt.Errorf("begin sending: resolve groups: %w", err)
	}

	requestedIDs := deliveryIDSet(rows)

	groups, transitions, err := buildStartedGroups(resolved, requestedIDs, startedAt)
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

	touched, err := applyRowTransitions(ctx, tx, "begin sending", transitions)
	if err != nil {
		return StartedOperation{}, transitions, nil, fmt.Errorf("begin sending: apply rows: %w", err)
	}

	return StartedOperation{groups: groups, startedAt: startedAt}, transitions, touched, nil
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
	startedAt time.Time,
) ([]startedLogicalGroup, []rowTransition, error) {
	groups := make([]startedLogicalGroup, 0, len(resolved.resolutions))
	transitions := make([]rowTransition, 0, len(resolved.resolutions))

	for i := range resolved.resolutions {
		group, transition, err := buildStartedGroup(resolved.resolutions[i], resolved, requestedIDs, startedAt)
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
	startedAt time.Time,
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

	if before.Status != lifecycle.StatusPending || before.LockedAt == nil || before.RowVersion <= 0 {
		return startedLogicalGroup{}, rowTransition{}, &transitionConflictError{
			operation: "begin sending", detail: fmt.Sprintf("owner %d has no valid preparation fence", before.ID),
		}
	}

	followers, err := startedFollowers(resolution, resolved.rowsByID)
	if err != nil {
		return startedLogicalGroup{}, rowTransition{}, fmt.Errorf("begin sending: load followers: %w", err)
	}

	after := sendingTransitionRow(before, startedAt)
	group := startedLogicalGroup{
		key: resolution.Key(), ownerBefore: before, ownerAfter: after,
		followers: followers, ledgerBefore: ledgerRecordPointer(resolved.ledgerByKey, resolution), providerOwner: true,
	}

	return group, rowTransition{before: before, after: after}, nil
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

func sendingTransitionRow(before transitionRow, startedAt time.Time) transitionRow {
	after := before

	after.Status = lifecycle.StatusSending
	after.RowVersion++

	after.LockedAt = &startedAt
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
	if err := deliverysql.SelectDeliverySQL(
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

func sameTransitionState(actual, expected transitionRow) bool {
	return actual.ID == expected.ID &&
		actual.OutboxID == expected.OutboxID &&
		actual.Status == expected.Status &&
		actual.AttemptCount == expected.AttemptCount &&
		actual.RowVersion == expected.RowVersion &&
		timesEqual(actual.LockedAt, expected.LockedAt) &&
		timesEqual(actual.SentAt, expected.SentAt) &&
		actual.NextAttemptAt.Equal(expected.NextAttemptAt) &&
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
