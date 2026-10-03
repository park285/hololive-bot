package workerobservability

import (
	"strings"
	"testing"
	"time"

	"github.com/park285/shared-go/v2/pkg/workercontract"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/kapu/hololive-shared/pkg/config/settingstest"
)

func registryWithObservationMetrics(t *testing.T) *workercontract.Registry {
	t.Helper()

	loaded := settingstest.LoadProfileFixture(t, "hololive", "api", "stack-worker-profile-api.json")
	idleWorkers := []string{"bot_reply_outbox", "bot_webhook_inbox"}

	// 관측 대상이 아닌 worker는 executor를 꺼 snapshot source 없이 빈 process queue로 등록한다.
	for _, workerID := range idleWorkers {
		worker := loaded.Profile.Workers[workerID]

		worker.Executor.Enabled = false
		loaded.Profile.Workers[workerID] = worker
	}

	tracker := workercontract.NewExecutorTracker()
	tracker.StartWorkers(2)

	counters := &workercontract.Counters{}
	counters.RecordAdmission(workercontract.AdmissionRejected)

	registry := workercontract.NewRegistry(loaded, workercontract.NewProfileFileChecker(loaded, time.Now()))
	if err := registry.Register(workercontract.Registration{
		WorkerID:                "source_observation",
		Runtime:                 workercontract.RuntimeGo,
		QueueBackend:            workercontract.QueueMemory,
		QueueScope:              workercontract.QueueScopeProcess,
		SettingsValidated:       true,
		PerJobDeadlineValidated: true,
		ExecutorSnapshot:        func() workercontract.ExecutorSnapshot { return tracker.Snapshot(time.Now()) },
		QueueSnapshot: func() workercontract.QueueSnapshot {
			return workercontract.CurrentQueueSnapshot(2, 3*time.Second, time.Now())
		},
		Counters: counters,
	}); err != nil {
		t.Fatal(err)
	}

	for _, workerID := range idleWorkers {
		if err := registry.Register(workercontract.Registration{
			WorkerID:          workerID,
			Runtime:           workercontract.RuntimeGo,
			QueueBackend:      workercontract.QueueMemory,
			QueueScope:        workercontract.QueueScopeProcess,
			SettingsValidated: true,
		}); err != nil {
			t.Fatal(err)
		}
	}

	if err := registry.Seal(); err != nil {
		t.Fatal(err)
	}

	return registry
}

func TestGathererExposesCommonWorkerFamilies(t *testing.T) {
	registry := registryWithObservationMetrics(t)

	want := `# HELP iris_stack_worker_configured_workers Configured executor concurrency for this process.
# TYPE iris_stack_worker_configured_workers gauge
iris_stack_worker_configured_workers{queue_backend="memory",queue_scope="process",runtime="go",stack_role="api",stack_service="hololive",worker="bot_reply_outbox"} 16
iris_stack_worker_configured_workers{queue_backend="memory",queue_scope="process",runtime="go",stack_role="api",stack_service="hololive",worker="bot_webhook_inbox"} 16
iris_stack_worker_configured_workers{queue_backend="memory",queue_scope="process",runtime="go",stack_role="api",stack_service="hololive",worker="source_observation"} 2
# HELP iris_stack_worker_queue_depth Current ready canonical queue depth.
# TYPE iris_stack_worker_queue_depth gauge
iris_stack_worker_queue_depth{queue_backend="memory",queue_scope="process",runtime="go",stack_role="api",stack_service="hololive",worker="bot_reply_outbox"} 0
iris_stack_worker_queue_depth{queue_backend="memory",queue_scope="process",runtime="go",stack_role="api",stack_service="hololive",worker="bot_webhook_inbox"} 0
iris_stack_worker_queue_depth{queue_backend="memory",queue_scope="process",runtime="go",stack_role="api",stack_service="hololive",worker="source_observation"} 2
# HELP iris_stack_worker_admissions_total Canonical queue admissions by ownership result.
# TYPE iris_stack_worker_admissions_total counter
iris_stack_worker_admissions_total{queue_backend="memory",queue_scope="process",result="accepted",runtime="go",stack_role="api",stack_service="hololive",worker="bot_reply_outbox"} 0
iris_stack_worker_admissions_total{queue_backend="memory",queue_scope="process",result="accepted",runtime="go",stack_role="api",stack_service="hololive",worker="bot_webhook_inbox"} 0
iris_stack_worker_admissions_total{queue_backend="memory",queue_scope="process",result="accepted",runtime="go",stack_role="api",stack_service="hololive",worker="source_observation"} 0
iris_stack_worker_admissions_total{queue_backend="memory",queue_scope="process",result="duplicate",runtime="go",stack_role="api",stack_service="hololive",worker="bot_reply_outbox"} 0
iris_stack_worker_admissions_total{queue_backend="memory",queue_scope="process",result="duplicate",runtime="go",stack_role="api",stack_service="hololive",worker="bot_webhook_inbox"} 0
iris_stack_worker_admissions_total{queue_backend="memory",queue_scope="process",result="duplicate",runtime="go",stack_role="api",stack_service="hololive",worker="source_observation"} 0
iris_stack_worker_admissions_total{queue_backend="memory",queue_scope="process",result="failed",runtime="go",stack_role="api",stack_service="hololive",worker="bot_reply_outbox"} 0
iris_stack_worker_admissions_total{queue_backend="memory",queue_scope="process",result="failed",runtime="go",stack_role="api",stack_service="hololive",worker="bot_webhook_inbox"} 0
iris_stack_worker_admissions_total{queue_backend="memory",queue_scope="process",result="failed",runtime="go",stack_role="api",stack_service="hololive",worker="source_observation"} 0
iris_stack_worker_admissions_total{queue_backend="memory",queue_scope="process",result="outcome_unknown",runtime="go",stack_role="api",stack_service="hololive",worker="bot_reply_outbox"} 0
iris_stack_worker_admissions_total{queue_backend="memory",queue_scope="process",result="outcome_unknown",runtime="go",stack_role="api",stack_service="hololive",worker="bot_webhook_inbox"} 0
iris_stack_worker_admissions_total{queue_backend="memory",queue_scope="process",result="outcome_unknown",runtime="go",stack_role="api",stack_service="hololive",worker="source_observation"} 0
iris_stack_worker_admissions_total{queue_backend="memory",queue_scope="process",result="rejected",runtime="go",stack_role="api",stack_service="hololive",worker="bot_reply_outbox"} 0
iris_stack_worker_admissions_total{queue_backend="memory",queue_scope="process",result="rejected",runtime="go",stack_role="api",stack_service="hololive",worker="bot_webhook_inbox"} 0
iris_stack_worker_admissions_total{queue_backend="memory",queue_scope="process",result="rejected",runtime="go",stack_role="api",stack_service="hololive",worker="source_observation"} 1
# HELP iris_stack_worker_running_workers Currently running executor workers in this process.
# TYPE iris_stack_worker_running_workers gauge
iris_stack_worker_running_workers{queue_backend="memory",queue_scope="process",runtime="go",stack_role="api",stack_service="hololive",worker="bot_reply_outbox"} 0
iris_stack_worker_running_workers{queue_backend="memory",queue_scope="process",runtime="go",stack_role="api",stack_service="hololive",worker="bot_webhook_inbox"} 0
iris_stack_worker_running_workers{queue_backend="memory",queue_scope="process",runtime="go",stack_role="api",stack_service="hololive",worker="source_observation"} 2
`
	if err := testutil.GatherAndCompare(
		NewGatherer(registry), strings.NewReader(want),
		"iris_stack_worker_configured_workers",
		"iris_stack_worker_queue_depth",
		"iris_stack_worker_admissions_total",
		"iris_stack_worker_running_workers",
	); err != nil {
		t.Fatal(err)
	}
}
