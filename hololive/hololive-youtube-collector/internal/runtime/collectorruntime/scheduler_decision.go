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

// decideRunFailure는 join된 callback 오류만 판정한다. 검증 실패는 소유권·취소 오류보다 먼저
// terminal 처리하고, 그 밖의 fatal 보고 여부는 release·defer 여부와 독립적으로 보존한다.
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

// decideLeaseRunOutcome는 repository가 확정한 join·release 결과를 오류 내용보다 우선한다.
// Cleanup timeout에는 callback이 아직 실행 중일 수 있으므로 callback 실패 처리로 되돌리지 않는다.
func decideLeaseRunOutcome(result joblease.LeaseRunResult) leaseRunDecision {
	decision := leaseRunDecision{handled: true}

	switch result.Outcome {
	case joblease.LeaseRunCallbackCompleted:
		return decision
	case joblease.LeaseRunFenceLost:
		// renew의 소유 손실은 callback에 섞인 publish/collect 손실과 중복 집계하지 않는다.
		decision.leaseLostPhase = phaseRenew
	case joblease.LeaseRunReleasedAfterSuperseded:
		// membership 무효로 release한 결과는 lease 소유 손실이 아니다.
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

	// Join된 release/context 오류의 순서가 callback의 fatal 분류를 가리지 않게 합니다.
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
