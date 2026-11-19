package collectorruntime

import (
	"context"
	"errors"
	"slices"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collection"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/joblease"
)

type runFailureAction uint8

const (
	runFailureIgnore runFailureAction = iota
	runFailureDefer
	runFailureDeferInvariant
	runFailureReleaseSuperseded
)

type runFailureDecision struct {
	action runFailureAction
	fatal  *FatalRuntimeError
}

func decideRunFailure(err error) runFailureDecision {
	if failure, ok := errors.AsType[*FatalRuntimeError](err); ok && failure.Phase == "result_validation" {
		return runFailureDecision{action: runFailureDeferInvariant, fatal: failure}
	}

	decision := runFailureDecision{action: runFailureDefer}

	if fatalCollectionError(err) {
		decision.fatal = &FatalRuntimeError{Phase: "collection", Err: err}
	}

	switch {
	case supersededError(err):
		decision.action = runFailureReleaseSuperseded
	case ignoreRunError(err):
		decision.action = runFailureIgnore
	}

	return decision
}

type leaseRunDecision struct {
	handled          bool
	leaseLostPhase   string
	supervisionPhase string
}

func decideLeaseRunOutcome(result joblease.LeaseRunResult) leaseRunDecision {
	decision := leaseRunDecision{handled: true}

	switch result.Outcome {
	case joblease.LeaseRunCallbackCompleted:
		return decision
	case joblease.LeaseRunFenceLost:
		decision.leaseLostPhase = phaseRenew
	case joblease.LeaseRunReleasedAfterSuperseded:
	case joblease.LeaseRunReleasedAfterParentCancel:
		decision.leaseLostPhase = fenceLostPhase(result.Err)
	case joblease.LeaseRunReleasedAfterRenewFailure:
		decision.leaseLostPhase = phaseRenew
		decision.supervisionPhase = phaseRenew

		return decision
	case joblease.LeaseRunCleanupTimedOut:
		decision.supervisionPhase = "cleanup"

		return decision
	case joblease.LeaseRunCallbackFailed:
		return leaseRunDecision{}
	default:
		return leaseRunDecision{}
	}

	if fatalCollectionError(result.Err) {
		decision.supervisionPhase = "cleanup"
	}

	return decision
}

func fatalCollectionError(err error) bool {
	if err == nil {
		return false
	}

	class := collecterr.ClassOf(err)
	if !collecterr.IsUnclassified(err) && (class == contract.ClassInternal || class == contract.ClassProtocol) {
		return true
	}

	if joined, ok := errors.AsType[interface {
		error
		Unwrap() []error
	}](err); ok {
		return slices.ContainsFunc(joined.Unwrap(), fatalCollectionError)
	}

	return false
}

func ignoreRunError(err error) bool {
	return err == nil ||
		errors.Is(err, context.Canceled) ||
		errors.Is(err, collection.ErrFenceLost) ||
		supersededError(err)
}

func fenceLostPhase(err error) string {
	if !errors.Is(err, collection.ErrFenceLost) {
		return ""
	}

	if marked, ok := errors.AsType[*leasePhaseError](err); ok {
		return marked.phase
	}

	return phaseCollect
}
