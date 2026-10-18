package collectorruntime

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/park285/shared-go/v2/pkg/workercontract"

	collectorconfig "github.com/kapu/hololive-youtube-collector/internal/config"
)

func TestDisabledCollectionRegistryHasKnownEmptyProcessQueue(t *testing.T) {
	identity, err := workercontract.KnownIdentity("hololive", "youtube-collector")
	if err != nil {
		t.Fatal(err)
	}

	path, err := filepath.Abs(filepath.Join("..", "..", "config", "testdata", "stack-worker-profile-youtube-collector.json"))
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

	registry, _, err := newCollectorWorkerRegistry(&collectorconfig.WorkerProfile{Loaded: loaded}, nil)
	if err != nil {
		t.Fatal(err)
	}

	envelope, err := registry.Diagnostics(time.Now())
	if err != nil {
		t.Fatal(err)
	}

	diagnostics := envelope.Workers["collection"]
	if diagnostics.Executor.RunningWorkers == nil || *diagnostics.Executor.RunningWorkers != 0 ||
		diagnostics.Executor.InFlight == nil || *diagnostics.Executor.InFlight != 0 {
		t.Fatalf("disabled executor = %#v", diagnostics.Executor)
	}

	if diagnostics.Queue.Depth == nil || *diagnostics.Queue.Depth != 0 || diagnostics.Queue.Scope != workercontract.QueueScopeProcess {
		t.Fatalf("disabled queue = %#v", diagnostics.Queue)
	}
}

func TestCollectorRegistryObservesSourceReplacementWithoutReload(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "config", "testdata", "stack-worker-profile-youtube-collector.json"))
	if err != nil {
		t.Fatal(err)
	}

	raw = bytes.Replace(raw, []byte(`"enabled": true`), []byte(`"enabled": false`), 1)

	directory := t.TempDir()

	root, err := os.OpenRoot(directory)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		if closeErr := root.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	})

	effective := filepath.Join(directory, "release.json")
	source := filepath.Join(directory, "desired.json")

	for _, name := range []string{"release.json", "desired.json"} {
		if writeErr := root.WriteFile(name, raw, 0o600); writeErr != nil {
			t.Fatal(writeErr)
		}
	}

	t.Setenv(workercontract.ProfileFileEnv, effective)
	t.Setenv(workercontract.ProfileSourceFileEnv, source)

	profile, err := collectorconfig.LoadWorkerProfile()
	if err != nil {
		t.Fatal(err)
	}

	registry, checker, err := newCollectorWorkerRegistry(profile, nil)
	if err != nil {
		t.Fatal(err)
	}

	if !checker.Check(time.Now()).Match {
		t.Fatal("initial source must match the effective profile")
	}

	if writeErr := root.WriteFile("replacement.json", append(raw, '\n'), 0o600); writeErr != nil {
		t.Fatal(writeErr)
	}

	if renameErr := root.Rename("replacement.json", "desired.json"); renameErr != nil {
		t.Fatal(renameErr)
	}

	status := checker.Check(time.Now())
	if status.Match || status.ErrorCode == nil || *status.ErrorCode != workercontract.ProfileFileChanged {
		t.Fatalf("source replacement status = %#v", status)
	}

	envelope, err := registry.Diagnostics(time.Now())
	if err != nil {
		t.Fatal(err)
	}

	if envelope.Profile.Hash != profile.Loaded.Hash || envelope.Profile.FileMatch {
		t.Fatalf("loaded identity changed or source drift lost: %#v", envelope.Profile)
	}

	reloaded, err := collectorconfig.LoadWorkerProfile()
	if err != nil {
		t.Fatal(err)
	}

	if reloaded.Loaded.Hash != profile.Loaded.Hash {
		t.Fatal("desired source replaced the effective settings input")
	}
}
