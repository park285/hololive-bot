package collectorruntime

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collection"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/joblease"
)

const decisionTestValidationPhase = "result_validation"

func TestRunFailureDecisionCancellationEvidence(t *testing.T) {
	t.Parallel()

	canceled := collecterr.New(contract.ErrorCollectionCanceled, contract.ClassCanceled, "provider canceled")
	for _, test := range []struct {
		name   string
		err    error
		action runFailureAction
	}{
		{name: "diagnostic alone still defers", err: canceled, action: runFailureDefer},
		{name: "context cancellation skips defer", err: errors.Join(canceled, context.Canceled), action: runFailureIgnore},
		{name: "deadline still defers", err: context.DeadlineExceeded, action: runFailureDefer},
		{name: "cancellation wins over joined deadline", err: errors.Join(context.DeadlineExceeded, context.Canceled), action: runFailureIgnore},
		{name: "superseded wins over cancellation and fence", err: errors.Join(context.Canceled, collection.ErrFenceLost, collection.ErrTargetDisabled), action: runFailureReleaseSuperseded},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			decision := decideRunFailure(fmt.Errorf("callback: %w", test.err))
			if decision.action != test.action || decision.fatal != nil {
				t.Fatalf("decision = %+v, want action %v without fatal", decision, test.action)
			}
		})
	}
}

// 같은 joined error라도 repository가 확정한 소유권·join 결과에 따른다.
// Cleanup timeout에서 callback terminal 처리로 돌아가거나 renew/publish 손실을 중복 집계하지 않는다.
func TestLeaseRunDecisionUsesSupervisionEvidence(t *testing.T) {
	t.Parallel()

	cause := collecterr.New(contract.ErrorHelperProtocolMismatch, contract.ClassProtocol, "callback protocol")
	err := errors.Join(context.Canceled, collection.ErrTargetDisabled, markPublishLeaseError(collection.ErrFenceLost), cause)

	for _, test := range []struct {
		outcome          joblease.LeaseRunOutcome
		handled          bool
		leaseLostPhase   string
		supervisionPhase string
	}{
		{outcome: joblease.LeaseRunCallbackFailed},
		{outcome: joblease.LeaseRunFenceLost, handled: true, leaseLostPhase: phaseRenew, supervisionPhase: "cleanup"},
		{outcome: joblease.LeaseRunReleasedAfterSuperseded, handled: true, supervisionPhase: "cleanup"},
		{outcome: joblease.LeaseRunReleasedAfterParentCancel, handled: true, leaseLostPhase: phasePublish, supervisionPhase: "cleanup"},
		{outcome: joblease.LeaseRunReleasedAfterRenewFailure, handled: true, leaseLostPhase: phaseRenew, supervisionPhase: phaseRenew},
		{outcome: joblease.LeaseRunCleanupTimedOut, handled: true, supervisionPhase: "cleanup"},
	} {
		t.Run(string(test.outcome), func(t *testing.T) {
			t.Parallel()

			result := joblease.LeaseRunResult{Outcome: test.outcome, Err: err}
			decision := decideLeaseRunOutcome(result)

			if decision.handled != test.handled || decision.leaseLostPhase != test.leaseLostPhase || decision.supervisionPhase != test.supervisionPhase {
				t.Fatalf("decision = %+v, want handled=%t lost=%q supervision=%q", decision, test.handled, test.leaseLostPhase, test.supervisionPhase)
			}

			var fatal []error

			executor := newRunErrorExecutor(&fatal)
			registerer := prometheus.NewPedanticRegistry()

			executor.metrics = NewMetrics(registerer)

			if got := executor.handleLeaseRunOutcome(t.Context(), result, leaseMetricSpec(), &contract.LeaseProof{}); got != test.handled {
				t.Fatalf("handled = %t, want %t", got, test.handled)
			}

			for _, phase := range []string{phaseRenew, phaseCollect, phasePublish} {
				want := float64(0)

				if phase == test.leaseLostPhase {
					want = 1
				}

				if got := leaseLostByPhase(t, registerer, phase); got != want {
					t.Fatalf("%s lease lost = %v, want %v", phase, got, want)
				}
			}

			assertSupervisionFatal(t, fatal, cause, test.handled)
		})
	}
}

type orderedFailureLease struct {
	recordingLease

	events        *[]string
	releaseReason joblease.ReleaseReason
}

func (l *orderedFailureLease) Defer(ctx context.Context, input collection.DeferCollectionInput) error {
	*l.events = append(*l.events, "defer")

	return l.recordingLease.Defer(ctx, input)
}

func (l *orderedFailureLease) Release(_ context.Context, reason joblease.ReleaseReason) error {
	*l.events = append(*l.events, "release")
	l.releaseReason = reason

	return nil
}

func TestRunFailureFatalPrecedencePreservesTerminalOrder(t *testing.T) {
	t.Parallel()

	cause := collecterr.New(contract.ErrorInternalInvariant, contract.ClassInternal, "callback invariant")
	validation := &FatalRuntimeError{Phase: decisionTestValidationPhase, Err: cause}
	ownershipErr := errors.Join(context.Canceled, collection.ErrProjectionStale, collection.ErrFenceLost)

	for _, test := range []struct {
		name      string
		err       error
		wantPhase string
		events    []string
	}{
		{name: "validation before ownership", err: errors.Join(validation, ownershipErr), wantPhase: decisionTestValidationPhase, events: []string{"defer", "fatal"}},
		{name: "fatal before superseded release", err: errors.Join(ownershipErr, cause), wantPhase: "collection", events: []string{"fatal", "release"}},
		{name: "fatal before ordinary defer", err: cause, wantPhase: "collection", events: []string{"fatal", "defer"}},
		{name: "fatal survives cancellation", err: errors.Join(context.Canceled, cause), wantPhase: "collection", events: []string{"fatal"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			var (
				fatal  []error
				events []string
			)

			executor := newRunErrorExecutor(&fatal)

			executor.reportFatal = func(err error) {
				events = append(events, "fatal")
				fatal = append(fatal, err)
			}

			lease := &orderedFailureLease{events: &events}
			executor.handleRunError(t.Context(), lease, leaseMetricSpec(), &contract.LeaseProof{}, test.err)

			if !slices.Equal(events, test.events) {
				t.Fatalf("effects = %v, want %v", events, test.events)
			}

			if len(fatal) != 1 {
				t.Fatalf("fatal reports = %v, want one", fatal)
			}

			failure, ok := errors.AsType[*FatalRuntimeError](fatal[0])
			if !ok || failure.Phase != test.wantPhase || !errors.Is(failure, cause) {
				t.Fatalf("fatal = %v, want phase %q retaining cause", fatal[0], test.wantPhase)
			}

			if test.wantPhase == decisionTestValidationPhase && failure != validation {
				t.Fatal("validation failure identity was replaced")
			}

			if slices.Contains(test.events, "release") && lease.releaseReason != joblease.ReleaseSuperseded {
				t.Fatalf("release reason = %q, want superseded", lease.releaseReason)
			}

			if lease.deferCalls != 0 && (lease.deferCode != string(contract.ErrorInternalInvariant) || lease.deferClass != string(contract.ClassInternal)) {
				t.Fatalf("deferred tuple = %s/%s, want classified invariant", lease.deferCode, lease.deferClass)
			}
		})
	}
}
