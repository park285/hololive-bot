package settings

import (
	"path/filepath"
	"testing"

	"github.com/park285/shared-go/v2/pkg/workercontract"
)

func stackWorkerProfileFixture(t *testing.T, name string) string {
	t.Helper()

	path, err := filepath.Abs(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("resolve worker profile fixture: %v", err)
	}

	return path
}

func useStackWorkerProfileFixture(t *testing.T, name string) {
	t.Helper()
	t.Setenv(workercontract.ProfileFileEnv, stackWorkerProfileFixture(t, name))
}

func mustLoadWorkerProfileFixture(t *testing.T, service, role, name string) workercontract.LoadedProfile {
	t.Helper()

	identity, err := workercontract.KnownIdentity(service, role)
	if err != nil {
		t.Fatalf("resolve worker identity: %v", err)
	}

	loaded, err := workercontract.LoadProfileFile(stackWorkerProfileFixture(t, name), identity)
	if err != nil {
		t.Fatalf("load worker profile fixture: %v", err)
	}

	return loaded
}

func apiWorkerProfileFixture(t *testing.T) *APIWorkerProfile {
	t.Helper()

	return &APIWorkerProfile{Loaded: mustLoadWorkerProfileFixture(t, "hololive", "api", "stack-worker-profile-api.json")}
}

func alarmWorkerProfileFixture(t *testing.T) *AlarmWorkerProfile {
	t.Helper()

	return &AlarmWorkerProfile{Loaded: mustLoadWorkerProfileFixture(t, "hololive", "alarm-worker", "stack-worker-profile-alarm-worker.json")}
}

func mustLoadAPIWorkerProfile(t *testing.T) *APIWorkerProfile {
	t.Helper()
	useStackWorkerProfileFixture(t, "stack-worker-profile-api.json")

	profile, err := LoadAPIWorkerProfile()
	if err != nil {
		t.Fatalf("load API worker profile fixture: %v", err)
	}

	return profile
}

func mustLoadCollectorWorkerProfile(t *testing.T) *YouTubeCollectorWorkerProfile {
	t.Helper()
	useStackWorkerProfileFixture(t, "stack-worker-profile-youtube-collector.json")

	profile, err := LoadYouTubeCollectorWorkerProfile()
	if err != nil {
		t.Fatalf("load collector worker profile fixture: %v", err)
	}

	return profile
}
