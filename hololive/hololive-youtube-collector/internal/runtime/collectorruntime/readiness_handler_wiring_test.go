package collectorruntime

import (
	"testing"

	sharedserver "github.com/kapu/hololive-shared/pkg/server/httpserver"
)

func TestConfigureCapturesSchedulerTracker(t *testing.T) {
	tracker := &readinessTracker{}
	readiness := &collectorReadiness{scheduler: &leaseScheduler{readiness: tracker}}
	opts := &sharedserver.RuntimeRouterOptions{}

	readiness.configure(opts)

	if readiness.tracker != tracker {
		t.Fatal("tracker = distinct instance, want the scheduler tracker captured")
	}

	if opts.ReadyResponder == nil {
		t.Fatal("ReadyResponder = nil, want configured responder")
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
