package store

import (
	"context"
	"errors"

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

	if _, ok := errors.AsType[*transitionMissingError](applyErr); ok {
		return rowApplyAdjudication{result: applyResultWithRules(ApplyMissing, nil, rules)}
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
