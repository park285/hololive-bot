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
) (ApplyResult, error) {
	if !operation.Valid() {
		return newApplyResult(ApplyConflict, nil), errors.New("apply started failure: invalid operation")
	}

	result, err := s.applyFailure(ctx, "apply started failure", operation, kind, reason, retryAfter)
	if err != nil {
		return result, fmt.Errorf("apply started failure: %w", err)
	}

	return result, nil
}

func (s *TransitionStore) ScheduleKnownRetry(
	ctx context.Context,
	operation StartedOperation,
	reason lifecycle.Reason,
	retryAfter time.Duration,
) (ApplyResult, error) {
	result, err := s.ApplyStartedFailure(ctx, operation, lifecycle.FailureRetryable, reason, retryAfter)
	if err != nil {
		return result, fmt.Errorf("schedule known retry: %w", err)
	}

	return result, nil
}

func (s *TransitionStore) CompleteFailed(
	ctx context.Context,
	operation StartedOperation,
	reason lifecycle.Reason,
) (ApplyResult, error) {
	result, err := s.ApplyStartedFailure(ctx, operation, lifecycle.FailurePermanent, reason, 0)
	if err != nil {
		return result, fmt.Errorf("complete failed: %w", err)
	}

	return result, nil
}

func (s *TransitionStore) ApplyPreparedFailure(
	ctx context.Context,
	rows []domain.YouTubeNotificationDelivery,
	outboxByID map[int64]domain.YouTubeNotificationOutbox,
	kind lifecycle.FailureKind,
	reason lifecycle.Reason,
	retryAfter time.Duration,
) (ApplyResult, error) {
	if len(rows) == 0 {
		return newApplyResult(ApplyConflict, nil), errors.New("apply prepared failure: rows are empty")
	}

	if err := s.ensureReady(ctx); err != nil {
		return newApplyResult(ApplyIndeterminate, nil), fmt.Errorf("apply prepared failure: %w", err)
	}

	at, err := lifecycle.CanonicalTime(time.Now())
	if err != nil {
		return newApplyResult(ApplyIndeterminate, nil), fmt.Errorf("apply prepared failure: at: %w", err)
	}

	var operation StartedOperation

	err = s.executeTx(ctx, "load prepared failure envelope", func(tx dbx.Querier) error {
		var loadErr error

		operation, loadErr = s.loadPreparedFailureOperation(ctx, tx, rows, outboxByID, at)
		if loadErr != nil {
			return fmt.Errorf("load prepared failure envelope: transaction body: %w", loadErr)
		}

		return nil
	})
	if err != nil {
		if _, ok := errors.AsType[*transitionConflictError](err); ok {
			return newApplyResult(ApplyConflict, nil), nil
		}

		return newApplyResult(ApplyIndeterminate, nil), fmt.Errorf("apply prepared failure: load envelope: %w", err)
	}

	result, err := s.applyFailure(ctx, "apply prepared failure", operation, kind, reason, retryAfter)
	if err != nil {
		return result, fmt.Errorf("apply prepared failure: apply transition: %w", err)
	}

	return result, nil
}

func (s *TransitionStore) loadPreparedFailureOperation(
	ctx context.Context,
	tx dbx.Querier,
	rows []domain.YouTubeNotificationDelivery,
	outboxByID map[int64]domain.YouTubeNotificationOutbox,
	at time.Time,
) (StartedOperation, error) {
	resolved, err := s.resolveClaimedGroups(ctx, tx, rows, outboxByID, at)
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

	return StartedOperation{groups: groups, startedAt: at}, nil
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

func (s *TransitionStore) applyFailure(
	ctx context.Context,
	operationName string,
	operation StartedOperation,
	kind lifecycle.FailureKind,
	reason lifecycle.Reason,
	retryAfter time.Duration,
) (ApplyResult, error) {
	transitions, rules, errorOutcome, err := s.prepareFailureTransitions(
		ctx, operationName, operation, kind, reason, retryAfter,
	)
	if err != nil {
		return newApplyResult(errorOutcome, nil), fmt.Errorf("%s: prepare transition: %w", operationName, err)
	}

	result, err := s.applyFailureTransitions(ctx, operationName, transitions, rules)
	if err != nil {
		return result, fmt.Errorf("%s: apply transition: %w", operationName, err)
	}

	return result, nil
}

func (s *TransitionStore) prepareFailureTransitions(
	ctx context.Context,
	operationName string,
	operation StartedOperation,
	kind lifecycle.FailureKind,
	reason lifecycle.Reason,
	retryAfter time.Duration,
) ([]rowTransition, []lifecycle.RuleID, ApplyOutcome, error) {
	if kind == lifecycle.FailureOutcomeUnknown {
		return nil, nil, ApplyIndeterminate, errors.New("apply failure: outcome unknown has no transition")
	}

	if err := s.ensureReady(ctx); err != nil {
		return nil, nil, ApplyIndeterminate, fmt.Errorf("%s: %w", operationName, err)
	}

	at, err := lifecycle.CanonicalTime(time.Now())
	if err != nil {
		return nil, nil, ApplyIndeterminate, fmt.Errorf("%s: at: %w", operationName, err)
	}

	policy, err := lifecycle.NewRetryPolicy(s.config.MaxRetries, s.config.RetryBackoff)
	if err != nil {
		return nil, nil, ApplyIndeterminate, fmt.Errorf("%s: retry policy: %w", operationName, err)
	}

	transitions, rules, err := buildFailureTransitions(operationName, operation, policy, kind, reason, at, retryAfter)
	if err != nil {
		return nil, nil, ApplyConflict, fmt.Errorf("%s: build transitions: %w", operationName, err)
	}

	sortTransitionsByID(transitions)

	return transitions, rules, ApplyApplied, nil
}

func (s *TransitionStore) applyFailureTransitions(
	ctx context.Context,
	operationName string,
	transitions []rowTransition,
	rules []lifecycle.RuleID,
) (ApplyResult, error) {
	var priorAdjudication CommitAdjudication

	for attempt := 0; attempt <= transitionOperationRetryLimit; attempt++ {
		var touched []int64

		applyErr := s.executeTx(ctx, operationName, func(tx dbx.Querier) error {
			var err error

			touched, err = applyRowTransitions(ctx, tx, operationName, transitions)
			if err != nil {
				return fmt.Errorf("%s: apply rows: %w", operationName, err)
			}

			return nil
		})
		if applyErr == nil {
			result := newApplyResultWithRules(ApplyApplied, touched, rules)

			result.CommitAdjudication = priorAdjudication

			return result, nil
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

func buildFailureTransitions(
	operationName string,
	operation StartedOperation,
	policy lifecycle.RetryPolicy,
	kind lifecycle.FailureKind,
	reason lifecycle.Reason,
	at time.Time,
	retryAfter time.Duration,
) ([]rowTransition, []lifecycle.RuleID, error) {
	transitions := make([]rowTransition, 0)
	rules := make([]lifecycle.RuleID, 0, len(operation.groups))

	for i := range operation.groups {
		groupTransitions, rule, err := failureGroupTransitions(
			operationName, operation.groups[i], policy, kind, reason, at, retryAfter,
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
	at time.Time,
	retryAfter time.Duration,
) ([]rowTransition, lifecycle.RuleID, error) {
	owner := transitionLifecycleSnapshot(group.ownerAfter)
	followers := lifecycleSnapshots(group.followers)

	decision, err := lifecycle.EvaluateFailure(policy, owner, followers, kind, at, retryAfter)
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
	after.NextAttemptAt = mutation.NextAttemptAt()

	if mutation.ClearsLock() {
		after.LockedAt = nil
	}

	after.SentAt = nil
	after.Error = string(reason)

	return rowTransition{before: before, after: after}
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
