package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/lifecycle"
	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/preparation"
	"github.com/kapu/hololive-shared/pkg/dbx"
	ytcontentid "github.com/kapu/hololive-shared/pkg/service/youtube/contentid"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/deliverysql"
)

type ReviveResult struct {
	ApplyResult

	RevivedLogicalGroups int
	RevivedDeliveries    int
	Blocked              []BlockedLogicalGroup
}

func (s *TransitionStore) ReviveFailedLogicalGroups(
	ctx context.Context,
	freshnessWindow time.Duration,
	limit int,
) (ReviveResult, error) {
	if err := s.ensureReady(ctx); err != nil {
		return ReviveResult{ApplyResult: newApplyResult(ApplyIndeterminate, nil)}, fmt.Errorf("revive failed logical groups: %w", err)
	}

	policy, err := lifecycle.NewRevivePolicy(true, freshnessWindow, s.config.LogicalGroupLimit)
	if err != nil {
		return ReviveResult{ApplyResult: newApplyResult(ApplyConflict, nil)}, fmt.Errorf("revive failed logical groups: policy: %w", err)
	}

	if limit <= 0 {
		return ReviveResult{ApplyResult: newApplyResult(ApplyConflict, nil)}, errors.New("revive failed logical groups: limit must be positive")
	}

	at, err := lifecycle.CanonicalTime(time.Now())
	if err != nil {
		return ReviveResult{ApplyResult: newApplyResult(ApplyIndeterminate, nil)}, fmt.Errorf("revive failed logical groups: at: %w", err)
	}

	var priorAdjudication CommitAdjudication

	for attempt := 0; attempt <= transitionOperationRetryLimit; attempt++ {
		result, transitions, applyErr := s.reviveFailedOnce(ctx, policy, at, limit)
		if applyErr == nil {
			result.CommitAdjudication = priorAdjudication

			return result, nil
		}

		decision := s.adjudicateRowApply(
			ctx,
			"revive failed logical groups",
			"logical groups contain mixed FAILED pre-state and PENDING post-state",
			transitions,
			result.Rules,
			applyErr,
			priorAdjudication,
			attempt,
		)
		if decision.retry {
			priorAdjudication = decision.prior

			continue
		}

		result.ApplyResult = decision.result

		return result, decision.err
	}

	return ReviveResult{ApplyResult: newApplyResult(ApplyIndeterminate, nil)}, errors.New("revive failed logical groups: retry loop exhausted")
}

func (s *TransitionStore) reviveFailedOnce(
	ctx context.Context,
	policy lifecycle.RevivePolicy,
	at time.Time,
	limit int,
) (ReviveResult, []rowTransition, error) {
	var (
		result      ReviveResult
		transitions []rowTransition
	)

	err := s.executeTx(ctx, "revive failed logical groups", func(tx dbx.Querier) error {
		var txErr error

		result, transitions, txErr = s.reviveFailedTx(ctx, tx, policy, at, limit)
		if txErr != nil {
			return fmt.Errorf("revive failed logical groups: transaction body: %w", txErr)
		}

		return nil
	})
	if err != nil {
		return result, transitions, fmt.Errorf("revive failed logical groups: execute transaction: %w", err)
	}

	return result, transitions, nil
}

func (s *TransitionStore) reviveFailedTx(
	ctx context.Context,
	tx dbx.Querier,
	policy lifecycle.RevivePolicy,
	at time.Time,
	limit int,
) (ReviveResult, []rowTransition, error) {
	candidates, err := loadReviveCandidates(
		ctx, tx, at.Add(-s.config.LockTimeout), at.Add(-policy.FreshnessWindow()), limit,
	)
	if err != nil {
		return ReviveResult{}, nil, fmt.Errorf("revive failed logical groups: load candidates: %w", err)
	}

	if len(candidates) == 0 {
		return ReviveResult{ApplyResult: newApplyResult(ApplyApplied, nil)}, nil, nil
	}

	resolved, err := s.resolveStaleSendingGroups(ctx, tx, candidates, at)
	if err != nil {
		return ReviveResult{}, nil, fmt.Errorf("revive failed logical groups: resolve: %w", err)
	}

	result, transitions, rules, err := s.buildReviveChanges(resolved, candidates, policy, at)
	if err != nil {
		return result, transitions, fmt.Errorf("revive failed logical groups: build changes: %w", err)
	}

	sortTransitionsByID(transitions)

	touched, err := applyRowTransitions(ctx, tx, "revive failed logical groups", transitions)
	if err != nil {
		return result, transitions, fmt.Errorf("revive failed logical groups: apply rows: %w", err)
	}

	result.ApplyResult = newApplyResultWithRules(ApplyApplied, touched, rules)
	result.RevivedDeliveries = len(transitions)

	return result, transitions, nil
}

func (s *TransitionStore) buildReviveChanges(
	resolved resolvedLogicalGroups,
	candidates []transitionRow,
	policy lifecycle.RevivePolicy,
	at time.Time,
) (ReviveResult, []rowTransition, []lifecycle.RuleID, error) {
	candidateKeys, err := transitionRowKeySet(candidates)
	if err != nil {
		return ReviveResult{}, nil, nil, fmt.Errorf("revive failed logical groups: candidate keys: %w", err)
	}

	var (
		result      ReviveResult
		transitions []rowTransition
		rules       []lifecycle.RuleID
	)

	for i := range resolved.resolutions {
		change, err := s.reviveResolution(resolved.resolutions[i], resolved, candidateKeys, policy, at)
		if err != nil {
			return result, transitions, rules, fmt.Errorf("revive failed logical groups: resolution %d: %w", i, err)
		}

		transitions = append(transitions, change.transitions...)
		if change.rule != "" {
			rules = append(rules, change.rule)
		}

		if change.blocked != nil {
			result.Blocked = append(result.Blocked, *change.blocked)
		}

		if change.revived {
			result.RevivedLogicalGroups++
		}
	}

	return result, transitions, rules, nil
}

type reviveResolutionChange struct {
	transitions []rowTransition
	rule        lifecycle.RuleID
	blocked     *BlockedLogicalGroup
	revived     bool
}

func (s *TransitionStore) reviveResolution(
	resolution preparation.Resolution,
	resolved resolvedLogicalGroups,
	candidateKeys map[ytcontentid.LogicalKey]struct{},
	policy lifecycle.RevivePolicy,
	at time.Time,
) (reviveResolutionChange, error) {
	if _, selected := candidateKeys[resolution.Key()]; !selected {
		return reviveResolutionChange{}, nil
	}

	if resolution.Kind() == preparation.LogicalInvariantBreach {
		blocked := &BlockedLogicalGroup{KeyHash: resolution.Key().Hash(), Reason: resolution.InvariantReason()}

		return reviveResolutionChange{blocked: blocked}, nil
	}

	if resolution.Kind() != preparation.LogicalFailed {
		return reviveResolutionChange{}, nil
	}

	if _, ledgerPresent := resolved.ledgerByKey[resolution.Key()]; ledgerPresent {
		return reviveResolutionChange{}, nil
	}

	evaluation, err := s.evaluateReviveResolution(resolution, resolved.rowsByID, policy, at)
	if err != nil {
		return reviveResolutionChange{}, fmt.Errorf("revive failed logical groups: evaluate resolution: %w", err)
	}

	if !evaluation.evaluated {
		return reviveResolutionChange{}, nil
	}

	transitions, err := reviveDecisionTransitions(evaluation.decision, evaluation.groupRows)
	if err != nil {
		return reviveResolutionChange{}, fmt.Errorf("revive failed logical groups: build decision transitions: %w", err)
	}

	return reviveResolutionChange{transitions: transitions, rule: evaluation.decision.RuleID(), revived: true}, nil
}

type reviveEvaluation struct {
	decision  lifecycle.Decision
	groupRows map[int64]transitionRow
	evaluated bool
}

func (s *TransitionStore) evaluateReviveResolution(
	resolution preparation.Resolution,
	rowsByID map[int64]transitionRow,
	policy lifecycle.RevivePolicy,
	at time.Time,
) (reviveEvaluation, error) {
	owner, err := transitionRowFromSnapshot(resolution.Owner(), rowsByID)
	if err != nil {
		return reviveEvaluation{}, fmt.Errorf("revive failed logical groups: owner row: %w", err)
	}

	groupRows, followerSnapshots, hasActiveLock, outboxNeverSent, err := s.reviveGroupEvidence(
		owner, resolution.Followers(), rowsByID, at,
	)
	if err != nil {
		return reviveEvaluation{}, fmt.Errorf("revive failed logical groups: group evidence: %w", err)
	}

	decision, evaluated := evaluateRevivePolicy(policy, lifecycle.ReviveInput{
		Owner: transitionLifecycleSnapshot(owner), Followers: followerSnapshots,
		LedgerPresent: false, OutboxNeverSent: outboxNeverSent,
		SourceObservedAt: owner.OutboxCreatedAt, HasActiveLock: hasActiveLock,
	}, at)
	if !evaluated {
		return reviveEvaluation{}, nil
	}

	return reviveEvaluation{decision: decision, groupRows: groupRows, evaluated: true}, nil
}

func evaluateRevivePolicy(
	policy lifecycle.RevivePolicy,
	input lifecycle.ReviveInput,
	at time.Time,
) (lifecycle.Decision, bool) {
	decision, err := lifecycle.EvaluateRevive(policy, input, at)

	return decision, err == nil
}

func (s *TransitionStore) reviveGroupEvidence(
	owner transitionRow,
	followers []preparation.DeliverySnapshot,
	rowsByID map[int64]transitionRow,
	at time.Time,
) (map[int64]transitionRow, []lifecycle.RowSnapshot, bool, bool, error) {
	groupRows := map[int64]transitionRow{owner.ID: owner}
	snapshots := make([]lifecycle.RowSnapshot, 0, len(followers))
	hasActiveLock := activeLogicalGroupLock(owner, at, s.config.LockTimeout)
	outboxNeverSent := owner.OutboxSentAt == nil

	for i := range followers {
		row, err := transitionRowFromSnapshot(followers[i], rowsByID)
		if err != nil {
			return nil, nil, false, false, fmt.Errorf("revive failed logical groups: follower row: %w", err)
		}

		groupRows[row.ID] = row
		snapshots = append(snapshots, transitionLifecycleSnapshot(row))
		hasActiveLock = hasActiveLock || activeLogicalGroupLock(row, at, s.config.LockTimeout)
		outboxNeverSent = outboxNeverSent && row.OutboxSentAt == nil
	}

	return groupRows, snapshots, hasActiveLock, outboxNeverSent, nil
}

func reviveDecisionTransitions(
	decision lifecycle.Decision,
	groupRows map[int64]transitionRow,
) ([]rowTransition, error) {
	transitions := make([]rowTransition, 0, len(decision.Mutations()))
	mutations := decision.Mutations()

	for i := range mutations {
		mutation := mutations[i]
		before, ok := groupRows[mutation.DeliveryID()]

		if !ok {
			return nil, &AtomicityBreachError{
				Operation: "revive failed logical groups", Detail: "revive mutation escaped logical group",
			}
		}

		after := before

		after.Status = mutation.NextStatus()
		after.RowVersion = mutation.NextVersion()
		after.AttemptCount = mutation.NextAttempt()
		after.NextAttemptAt = mutation.NextAttemptAt()
		after.LockedAt = nil
		after.SentAt = nil
		after.Error = ""
		transitions = append(transitions, rowTransition{before: before, after: after})
	}

	return transitions, nil
}

func transitionRowKeySet(rows []transitionRow) (map[ytcontentid.LogicalKey]struct{}, error) {
	keys := make(map[ytcontentid.LogicalKey]struct{}, len(rows))
	for i := range rows {
		key, err := rows[i].logicalKey()
		if err != nil {
			return nil, fmt.Errorf("revive failed logical groups: candidate identity: %w", err)
		}

		keys[key] = struct{}{}
	}

	return keys, nil
}

func loadReviveCandidates(
	ctx context.Context,
	db dbx.Querier,
	lockCutoff time.Time,
	freshCutoff time.Time,
	limit int,
) ([]transitionRow, error) {
	var candidates []transitionRow

	if err := deliverysql.SelectDeliverySQL(
		ctx,
		db,
		&candidates,
		"load failed logical group revive candidates",
		mustSQL("transition_revive_candidates.sql"),
		lifecycle.StatusFailed,
		lockCutoff,
		freshCutoff,
		limit,
	); err != nil {
		return nil, fmt.Errorf("load failed logical group revive candidates: %w", err)
	}

	return candidates, nil
}

func activeLogicalGroupLock(row transitionRow, at time.Time, timeout time.Duration) bool {
	return row.LockedAt != nil && !row.LockedAt.Before(at.Add(-timeout))
}
