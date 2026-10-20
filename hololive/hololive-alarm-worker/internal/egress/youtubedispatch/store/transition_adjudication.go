package store

import (
	"context"
	"errors"

	"github.com/kapu/hololive-alarm-worker/internal/egress/sendfence"
	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/lifecycle"
)

type rowApplyAdjudication struct {
	result ApplyResult
	err    error
	retry  bool
	prior  CommitAdjudication
}

func (s *TransitionStore) adjudicateRowApply(
	ctx context.Context,
	operation string,
	mixedDetail string,
	transitions []rowTransition,
	rules []lifecycle.RuleID,
	applyErr error,
	prior CommitAdjudication,
	attempt int,
) rowApplyAdjudication {
	if _, ok := errors.AsType[*transitionConflictError](applyErr); ok {
		return rowApplyAdjudication{result: applyResultWithRules(ApplyConflict, nil, rules)}
	}

	// fence 미스는 다른 실행·회수 경로가 행을 가져갔다는 증거라 호출자가 버릴 수 없게 오류로도 돌려준다.
	if miss, ok := errors.AsType[*sendfence.MissError](applyErr); ok {
		return rowApplyAdjudication{result: applyResultWithRules(fenceMissOutcome(miss), nil, rules), err: applyErr}
	}

	if len(transitions) == 0 {
		return rowApplyAdjudication{result: applyResultWithRules(ApplyIndeterminate, nil, rules), err: applyErr}
	}

	state, stateErr := s.classifyRowEnvelope(ctx, operation, transitions)
	if stateErr != nil {
		result := adjudicated(applyResultWithRules(ApplyIndeterminate, nil, rules), applyErr, CommitIndeterminate)

		return rowApplyAdjudication{result: result, err: errors.Join(applyErr, stateErr)}
	}

	return adjudicateEnvelopeState(operation, mixedDetail, transitions, rules, applyErr, prior, attempt, state)
}

func adjudicateEnvelopeState(
	operation string,
	mixedDetail string,
	transitions []rowTransition,
	rules []lifecycle.RuleID,
	applyErr error,
	prior CommitAdjudication,
	attempt int,
	state envelopeState,
) rowApplyAdjudication {
	switch state {
	case envelopePost:
		result := applyResultWithRules(ApplyApplied, transitionOutboxIDs(transitions), rules)

		return rowApplyAdjudication{result: adjudicated(result, applyErr, CommitConfirmedPost), prior: prior}
	case envelopePre:
		return adjudicatePreEnvelope(rules, applyErr, prior, attempt)
	case envelopeMissing:
		result := adjudicated(applyResultWithRules(ApplyMissing, nil, rules), applyErr, CommitMissing)

		return rowApplyAdjudication{result: result, prior: prior}
	case envelopeConflict:
		result := adjudicated(applyResultWithRules(ApplyConflict, nil, rules), applyErr, CommitConflict)

		return rowApplyAdjudication{result: result, prior: prior}
	case envelopeMixed:
		result := adjudicated(applyResultWithRules(ApplyIndeterminate, nil, rules), applyErr, CommitMixed)
		breach := &AtomicityBreachError{Operation: operation, Detail: mixedDetail}

		return rowApplyAdjudication{result: result, err: errors.Join(applyErr, breach), prior: prior}
	default:
		result := adjudicated(applyResultWithRules(ApplyIndeterminate, nil, rules), applyErr, CommitIndeterminate)

		return rowApplyAdjudication{result: result, err: applyErr, prior: prior}
	}
}

func adjudicatePreEnvelope(
	rules []lifecycle.RuleID,
	applyErr error,
	prior CommitAdjudication,
	attempt int,
) rowApplyAdjudication {
	confirmed := adjudicated(applyResultWithRules(ApplyConflict, nil, rules), applyErr, CommitConfirmedPre)
	if confirmed.CommitAdjudication != "" {
		prior = confirmed.CommitAdjudication
	}

	if attempt < transitionOperationRetryLimit {
		return rowApplyAdjudication{retry: true, prior: prior}
	}

	result := applyResultWithRules(ApplyConflict, nil, rules)

	result.CommitAdjudication = prior

	return rowApplyAdjudication{result: result, err: applyErr, prior: prior}
}

func applyResultWithRules(outcome ApplyOutcome, outboxIDs []int64, rules []lifecycle.RuleID) ApplyResult {
	if len(rules) == 0 {
		return newApplyResult(outcome, outboxIDs)
	}

	return newApplyResultWithRules(outcome, outboxIDs, rules)
}

// fenceMissOutcome은 대상이 모두 사라진 미스만 ApplyMissing으로, 나머지는 ApplyConflict로 보고한다.
func fenceMissOutcome(miss *sendfence.MissError) ApplyOutcome {
	for _, row := range miss.Rows {
		if row.Reason != sendfence.ReasonMissing {
			return ApplyConflict
		}
	}

	return ApplyMissing
}
