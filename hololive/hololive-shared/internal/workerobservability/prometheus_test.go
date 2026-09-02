package workerobservability

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/park285/shared-go/v2/pkg/workercontract"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestGathererExposesCommonWorkerFamilies(t *testing.T) {
	identity, err := workercontract.KnownIdentity("hololive", "youtube-collector")
	if err != nil {
		t.Fatal(err)
	}

	profilePath, err := filepath.Abs(filepath.Join("..", "..", "pkg", "config", "settings", "testdata", "stack-worker-profile-youtube-collector.json"))
	if err != nil {
		t.Fatal(err)
	}

	loaded, err := workercontract.LoadProfileFile(profilePath, identity)
	if err != nil {
		t.Fatal(err)
	}

	tracker := workercontract.NewExecutorTracker()
	tracker.StartWorkers(4)

	counters := &workercontract.Counters{}
	counters.RecordAdmission(workercontract.AdmissionRejected)

	registry := workercontract.NewRegistry(loaded, workercontract.NewProfileFileChecker(loaded, time.Now()))
	if err := registry.Register(workercontract.Registration{
		WorkerID:                "collection",
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

	if err := registry.Seal(); err != nil {
		t.Fatal(err)
	}

	want := `# HELP iris_stack_worker_configured_workers Configured executor concurrency for this process.
# TYPE iris_stack_worker_configured_workers gauge
iris_stack_worker_configured_workers{queue_backend="memory",queue_scope="process",runtime="go",stack_role="youtube-collector",stack_service="hololive",worker="collection"} 4
# HELP iris_stack_worker_queue_capacity Bounded canonical queue capacity in items.
# TYPE iris_stack_worker_queue_capacity gauge
iris_stack_worker_queue_capacity{queue_backend="memory",queue_scope="process",runtime="go",stack_role="youtube-collector",stack_service="hololive",worker="collection"} 16
# HELP iris_stack_worker_queue_depth Current ready canonical queue depth.
# TYPE iris_stack_worker_queue_depth gauge
iris_stack_worker_queue_depth{queue_backend="memory",queue_scope="process",runtime="go",stack_role="youtube-collector",stack_service="hololive",worker="collection"} 2
# HELP iris_stack_worker_admissions_total Canonical queue admissions by ownership result.
# TYPE iris_stack_worker_admissions_total counter
iris_stack_worker_admissions_total{queue_backend="memory",queue_scope="process",result="accepted",runtime="go",stack_role="youtube-collector",stack_service="hololive",worker="collection"} 0
iris_stack_worker_admissions_total{queue_backend="memory",queue_scope="process",result="duplicate",runtime="go",stack_role="youtube-collector",stack_service="hololive",worker="collection"} 0
iris_stack_worker_admissions_total{queue_backend="memory",queue_scope="process",result="failed",runtime="go",stack_role="youtube-collector",stack_service="hololive",worker="collection"} 0
iris_stack_worker_admissions_total{queue_backend="memory",queue_scope="process",result="outcome_unknown",runtime="go",stack_role="youtube-collector",stack_service="hololive",worker="collection"} 0
iris_stack_worker_admissions_total{queue_backend="memory",queue_scope="process",result="rejected",runtime="go",stack_role="youtube-collector",stack_service="hololive",worker="collection"} 1
# HELP iris_stack_worker_running_workers Currently running executor workers in this process.
# TYPE iris_stack_worker_running_workers gauge
iris_stack_worker_running_workers{queue_backend="memory",queue_scope="process",runtime="go",stack_role="youtube-collector",stack_service="hololive",worker="collection"} 4
`
	if err := testutil.GatherAndCompare(
		NewGatherer(registry), strings.NewReader(want),
		"iris_stack_worker_configured_workers",
		"iris_stack_worker_queue_capacity",
		"iris_stack_worker_queue_depth",
		"iris_stack_worker_admissions_total",
		"iris_stack_worker_running_workers",
	); err != nil {
		t.Fatal(err)
	}
}
