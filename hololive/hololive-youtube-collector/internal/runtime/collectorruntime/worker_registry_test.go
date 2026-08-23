package collectorruntime

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/config/settings"
	"github.com/park285/shared-go/v2/pkg/workercontract"
)

func TestDisabledCollectionRegistryHasKnownEmptyProcessQueue(t *testing.T) {
	identity, err := workercontract.KnownIdentity("hololive", "youtube-collector")
	if err != nil {
		t.Fatal(err)
	}
	path, err := filepath.Abs(filepath.Join("..", "..", "..", "..", "hololive-shared", "pkg", "config", "settings", "testdata", "stack-worker-profile-youtube-collector.json"))
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := workercontract.LoadProfileFile(path, identity)
	if err != nil {
		t.Fatal(err)
	}
	worker := loaded.Profile.Workers["collection"]
	worker.Executor.Enabled = false
	loaded.Profile.Workers["collection"] = worker
	registry, _, err := newCollectorWorkerRegistry(&settings.YouTubeCollectorWorkerProfile{Loaded: loaded}, nil)
	if err != nil {
		t.Fatal(err)
	}
	envelope, err := registry.Diagnostics(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	diagnostics := envelope.Workers["collection"]
	if diagnostics.Executor.RunningWorkers != 0 || diagnostics.Executor.InFlight != 0 {
		t.Fatalf("disabled executor = %#v", diagnostics.Executor)
	}
	if diagnostics.Queue.Depth == nil || *diagnostics.Queue.Depth != 0 || diagnostics.Queue.Scope != workercontract.QueueScopeProcess {
		t.Fatalf("disabled queue = %#v", diagnostics.Queue)
	}
}
