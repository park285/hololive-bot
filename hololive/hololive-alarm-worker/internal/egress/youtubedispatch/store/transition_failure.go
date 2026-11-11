package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/lifecycle"
	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/preparation"
	"github.com/kapu/hololive-shared/pkg/dbx"
	"github.com/kapu/hololive-shared/pkg/domain"
)

func (s *TransitionStore) ApplyStartedFailure(
	ctx context.Context,
	operation StartedOperation,
	kind lifecycle.FailureKind,
	reason lifecycle.Reason,
	retryAfter time.Duration,
	mode DeliveryMode,
) (ApplyResult, error) {
	if !operation.Valid() {
		return newApplyResult(ApplyConflict, nil), errors.New("apply started failure: invalid operation")
	}

	if err := mode.validateCallerMode(); err != nil {
		return newApplyResult(ApplyConflict, nil), fmt.Errorf("apply started failure: %w", err)
	}

	load := func(context.Context, dbx.Querier) (StartedOperation, error) { return operation, nil }

	result, err := s.applyFailure(ctx, "apply started failure", load, kind, reason, retryAfter, mode)
	if err != nil {
		return result, fmt.Errorf("apply started failure: %w", err)
	}

	return result, nil
}

// ApplyPreparedFailure는 claim 행의 논리 그룹을 실패 전이와 같은 트랜잭션에서 FOR UPDATE로 다시 읽고 claim fence를
// 확인한다. COMMIT 응답을 잃어 이전 상태로 판정된 재시도도 재조회부터 다시 한다.
func (s *TransitionStore) ApplyPreparedFailure(
	ctx context.Context,
	rows []domain.YouTubeNotificationDelivery,
	outboxByID map[int64]domain.YouTubeNotificationOutbox,
	kind lifecycle.FailureKind,
	reason lifecycle.Reason,
	retryAfter time.Duration,
	mode DeliveryMode,
) (ApplyResult, error) {
	if len(rows) == 0 {
		return newApplyResult(ApplyConflict, nil), errors.New("apply prepared failure: rows are empty")
	}

	if err := mode.validateCallerMode(); err != nil {
		return newApplyResult(ApplyConflict, nil), fmt.Errorf("apply prepared failure: %w", err)
	}

	load := func(ctx context.Context, tx dbx.Querier) (StartedOperation, error) {
		return s.loadPreparedFailureOperation(ctx, tx, rows, outboxByID)
	}

	result, err := s.applyFailure(ctx, "apply prepared failure", load, kind, reason, retryAfter, mode)
	if err != nil {
		return result, fmt.Errorf("apply prepared failure: %w", err)
	}

	return result, nil
}

func (s *TransitionStore) loadPreparedFailureOperation(
	ctx context.Context,
	tx dbx.Querier,
	rows []domain.YouTubeNotificationDelivery,
	outboxByID map[int64]domain.YouTubeNotificationOutbox,
) (StartedOperation, error) {
	resolved, err := s.resolveClaimedGroups(ctx, tx, "apply prepared failure", rows, outboxByID)
	if err != nil {
		return StartedOperation{}, fmt.Errorf("apply prepared failure: resolve groups: %w", err)
	}

	requestedIDs := deliveryIDSet(rows)
	groups := make([]startedLogicalGroup, 0, len(resolved.resolutions))

	for i := range resolved.resolutions {
		group, groupErr := preparedFailureGroup(resolved.resolutions[i], resolved.rowsByID, requestedIDs)
		if groupErr != nil {
			return StartedOperation{}, fmt.Errorf("apply prepared failure: build logical group: %w", groupErr)
		}

		groups = append(groups, group)
	}

	return StartedOperation{groups: groups}, nil
}

func preparedFailureGroup(
	resolution preparation.Resolution,
	rowsByID map[int64]transitionRow,
	requestedIDs map[int64]struct{},
) (startedLogicalGroup, error) {
	if resolution.Kind() != preparation.LogicalActive {
		return startedLogicalGroup{}, &transitionConflictError{
			operation: "apply prepared failure",
			detail:    fmt.Sprintf("logical group %s is no longer active", resolution.Key().Hash()),
		}
	}

	owner := resolution.Owner()
	if _, ok := requestedIDs[owner.DeliveryID]; !ok {
		return startedLogicalGroup{}, &transitionConflictError{
			operation: "apply prepared failure", detail: fmt.Sprintf("owner %d is not requested", owner.DeliveryID),
		}
	}

	ownerRow, err := transitionRowFromSnapshot(owner, rowsByID)
	if err != nil {
		return startedLogicalGroup{}, fmt.Errorf("apply prepared failure: owner row: %w", err)
	}

	followers, err := startedFollowers(resolution, rowsByID)
	if err != nil {
		return startedLogicalGroup{}, fmt.Errorf("apply prepared failure: follower rows: %w", err)
	}

	return startedLogicalGroup{
		key: resolution.Key(), ownerBefore: ownerRow, ownerAfter: ownerRow, followers: followers,
	}, nil
}

// applyFailure는 load가 이 트랜잭션에서 돌려준 논리 그룹의 실패 전이를 같은 트랜잭션에 기록한다. COMMIT 응답을 잃어
// 이전 상태로 판정되면 load부터 다시 실행한다. 전이를 만들지 못하면 판정 없이 prepareFailureTransitions가 정한 결과와
// 오류를 돌려준다.
func (s *TransitionStore) applyFailure(
	ctx context.Context,
	operationName string,
	load func(ctx context.Context, tx dbx.Querier) (StartedOperation, error),
	kind lifecycle.FailureKind,
	reason lifecycle.Reason,
	retryAfter time.Duration,
	mode DeliveryMode,
) (ApplyResult, error) {
	var priorAdjudication CommitAdjudication

	for attempt := 0; attempt <= transitionOperationRetryLimit; attempt++ {
		var (
			transitions    []rowTransition
			rules          []lifecycle.RuleID
			touched        []int64
			prepareOutcome ApplyOutcome
			prepareErr     error
		)

		applyErr := s.executeTx(ctx, operationName, func(tx dbx.Querier) error {
			operation, loadErr := load(ctx, tx)
			if loadErr != nil {
				return fmt.Errorf("%s: load groups: %w", operationName, loadErr)
			}

			transitions, rules, prepareOutcome, prepareErr = s.prepareFailureTransitions(
				operationName, operation, kind, reason, retryAfter,
			)
			if prepareErr != nil {
				return fmt.Errorf("%s: prepare transition: %w", operationName, prepareErr)
			}

			var rowsErr error

			touched, rowsErr = applyFailureRows(ctx, tx, operationName, operation, transitions, reason, mode)

			return rowsErr
		})
		if applyErr == nil {
			result := newApplyResultWithRules(ApplyApplied, touched, rules)

			result.CommitAdjudication = priorAdjudication

			return result, nil
		}

		if prepareErr != nil {
			return newApplyResult(prepareOutcome, nil), applyErr
		}

		decision := s.adjudicateRowApply(
			ctx,
			operationName,
			"logical group contains mixed pre-state and failure post-state",
			transitions,
			rules,
			applyErr,
			priorAdjudication,
			attempt,
		)
		if decision.retry {
			priorAdjudication = decision.prior

			continue
		}

		return decision.result, decision.err
	}

	return newApplyResult(ApplyIndeterminate, nil), fmt.Errorf("%s: retry loop exhausted", operationName)
}

// applyFailureRows는 실패 전이와 owner 시도 telemetry를 tx에 함께 기록한다. 시도 종료 시각은 전이 문장의 DB 시계다.
func applyFailureRows(
	ctx context.Context,
	tx dbx.Querier,
	operationName string,
	operation StartedOperation,
	transitions []rowTransition,
	reason lifecycle.Reason,
	mode DeliveryMode,
) ([]int64, error) {
	touched, failedAt, err := applyRowTransitions(ctx, tx, operationName, transitions)
	if err != nil {
		return nil, fmt.Errorf("%s: apply rows: %w", operationName, err)
	}

	// 실패 전이는 owner의 provider 실패와 prepared failure(message_missing, pre_send_claim 포함)를 모두 시도로 기록한다.
	attempts := ownerAttempts(operation.groups, attemptResultFailure, reason)
	if err := recordAttemptTelemetry(ctx, tx, mode, attempts, failedAt); err != nil {
		return nil, fmt.Errorf("%s: record telemetry: %w", operationName, err)
	}

	return touched, nil
}

func (s *TransitionStore) prepareFailureTransitions(
	operationName string,
	operation StartedOperation,
	kind lifecycle.FailureKind,
	reason lifecycle.Reason,
	retryAfter time.Duration,
) ([]rowTransition, []lifecycle.RuleID, ApplyOutcome, error) {
	if kind == lifecycle.FailureOutcomeUnknown {
		return nil, nil, ApplyIndeterminate, errors.New("apply failure: outcome unknown has no transition")
	}

	policy, err := lifecycle.NewRetryPolicy(s.config.MaxRetries, s.config.RetryBackoff)
	if err != nil {
		return nil, nil, ApplyIndeterminate, fmt.Errorf("%s: retry policy: %w", operationName, err)
	}

	transitions, rules, err := buildFailureTransitions(operationName, operation, policy, kind, reason, retryAfter)
	if err != nil {
		return nil, nil, ApplyConflict, fmt.Errorf("%s: build transitions: %w", operationName, err)
	}

	sortTransitionsByID(transitions)

	return transitions, rules, ApplyApplied, nil
}

func buildFailureTransitions(
	operationName string,
	operation StartedOperation,
	policy lifecycle.RetryPolicy,
	kind lifecycle.FailureKind,
	reason lifecycle.Reason,
	retryAfter time.Duration,
) ([]rowTransition, []lifecycle.RuleID, error) {
	transitions := make([]rowTransition, 0)
	rules := make([]lifecycle.RuleID, 0, len(operation.groups))

	for i := range operation.groups {
		groupTransitions, rule, err := failureGroupTransitions(
			operationName, operation.groups[i], policy, kind, reason, retryAfter,
		)
		if err != nil {
			return nil, nil, fmt.Errorf("%s: build group transitions: %w", operationName, err)
		}

		transitions = append(transitions, groupTransitions...)
		rules = append(rules, rule)
	}

	sortTransitionsByID(transitions)

	return transitions, rules, nil
}

func failureGroupTransitions(
	operationName string,
	group startedLogicalGroup,
	policy lifecycle.RetryPolicy,
	kind lifecycle.FailureKind,
	reason lifecycle.Reason,
	retryAfter time.Duration,
) ([]rowTransition, lifecycle.RuleID, error) {
	owner := transitionLifecycleSnapshot(group.ownerAfter)
	followers := lifecycleSnapshots(group.followers)

	decision, err := lifecycle.EvaluateFailure(policy, owner, followers, kind, retryAfter)
	if err != nil {
		return nil, "", fmt.Errorf("%s: evaluate logical group: %w", operationName, err)
	}

	transitions, err := failureDecisionTransitions(operationName, group, decision, reason)
	if err != nil {
		return nil, "", fmt.Errorf("%s: apply policy mutations: %w", operationName, err)
	}

	return transitions, decision.RuleID(), nil
}

func lifecycleSnapshots(rows []transitionRow) []lifecycle.RowSnapshot {
	snapshots := make([]lifecycle.RowSnapshot, 0, len(rows))
	for i := range rows {
		snapshots = append(snapshots, transitionLifecycleSnapshot(rows[i]))
	}

	return snapshots
}

func failureDecisionTransitions(
	operationName string,
	group startedLogicalGroup,
	decision lifecycle.Decision,
	reason lifecycle.Reason,
) ([]rowTransition, error) {
	groupRows := logicalGroupRows(group)
	transitions := make([]rowTransition, 0, len(decision.Mutations()))

	mutations := decision.Mutations()
	for i := range mutations {
		mutation := mutations[i]
		before, ok := groupRows[mutation.DeliveryID()]

		if !ok {
			return nil, &AtomicityBreachError{
				Operation: operationName, Detail: fmt.Sprintf("mutation delivery %d is outside its logical group", mutation.DeliveryID()),
			}
		}

		transitions = append(transitions, failureMutationTransition(before, mutation, reason))
	}

	return transitions, nil
}

func logicalGroupRows(group startedLogicalGroup) map[int64]transitionRow {
	rows := make(map[int64]transitionRow, len(group.followers)+1)

	rows[group.ownerAfter.ID] = group.ownerAfter

	for i := range group.followers {
		rows[group.followers[i].ID] = group.followers[i]
	}

	return rows
}

func failureMutationTransition(
	before transitionRow,
	mutation lifecycle.RowMutation,
	reason lifecycle.Reason,
) rowTransition {
	after := before

	after.Status = mutation.NextStatus()
	after.RowVersion = mutation.NextVersion()
	after.AttemptCount = mutation.NextAttempt()
	after.LockedAt = nil
	after.LockedBy = nil
	after.SentAt = nil
	after.Error = string(reason)

	return rowTransition{before: before, after: after, due: mutation.Due()}
}

func transitionLifecycleSnapshot(row transitionRow) lifecycle.RowSnapshot {
	lockedAt := time.Time{}

	if row.LockedAt != nil {
		lockedAt = row.LockedAt.UTC()
	}

	return lifecycle.RowSnapshot{
		DeliveryID: row.ID, Status: row.Status, AttemptCount: row.AttemptCount,
		NextAttemptAt: row.NextAttemptAt, LockedAt: lockedAt,
		RowVersion: row.RowVersion, CreatedAt: row.CreatedAt,
	}
}
