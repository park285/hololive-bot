package collectorruntime

import (
	"testing"

	collectorconfig "github.com/kapu/hololive-shared/pkg/config/settings/collector"
	sharedserver "github.com/kapu/hololive-shared/pkg/server/httpserver"
)

func TestConfigurePreservesInjectedTracker(t *testing.T) {
	tracker := &readinessTracker{}
	readiness := &collectorReadiness{scheduler: &leaseScheduler{readiness: tracker}, tracker: tracker}
	opts := &sharedserver.RuntimeRouterOptions{}

	readiness.configure(opts)

	if readiness.tracker != tracker {
		t.Fatal("tracker = distinct instance, want the scheduler tracker captured")
	}

	if opts.ReadyResponder == nil {
		t.Fatal("ReadyResponder = nil, want configured responder")
	}
}

func TestConfigureFeedsInjectedTrackerIntoReadinessEvaluation(t *testing.T) {
	scheduler := &leaseScheduler{readiness: &readinessTracker{}, state: SchedulerRunning}
	readiness := &collectorReadiness{scheduler: scheduler, tracker: scheduler.readiness}
	readiness.configure(&sharedserver.RuntimeRouterOptions{})

	cfg := collectorconfig.DefaultConfig()
	deps := readiness.deps(&cfg)

	if deps.tracker != scheduler.readiness {
		t.Fatal("deps.tracker = distinct instance, want the tracker captured by configure")
	}

	deps.helper = &stubHelper{}
	deps.store = &stubStore{}

	before := evaluateReadiness(t.Context(), &deps)
	if before.FirstSuccess || before.State != ReadyWaitingCollection || before.Dependency != "first_success" {
		t.Fatalf("before first success = %+v, want WAITING_COLLECTION first_success", before)
	}

	scheduler.recordTerminalSuccess(nil)

	after := evaluateReadiness(t.Context(), &deps)
	if !after.FirstSuccess || after.State != ReadyWaitingHandoff || after.Dependency != "observation_handoff" {
		t.Fatalf("after first success = %+v, want WAITING_HANDOFF observation_handoff", after)
	}
}

func TestConfigureDisabledWithoutSchedulerDoesNotPanic(t *testing.T) {
	readiness := &collectorReadiness{disabled: true}
	opts := &sharedserver.RuntimeRouterOptions{}

	readiness.configure(opts)

	if readiness.tracker != nil {
		t.Fatalf("tracker = %v, want nil on disabled runtime", readiness.tracker)
	}

	if opts.ReadyResponder == nil {
		t.Fatal("ReadyResponder = nil, want configured responder")
	}
}
