package config

import (
	"testing"

	"github.com/park285/shared-go/v2/pkg/workercontract"
)

const collectorProfileFixture = "testdata/stack-worker-profile-collector.json"

const collectorInstanceC = "collector-c"

func useCollectorProfileFixture(t *testing.T) {
	t.Helper()
	t.Setenv(workercontract.ProfileFileEnv, collectorProfileFixture)
}

func mustLoadCollectorWorkerProfile(t *testing.T) *WorkerProfile {
	t.Helper()
	useCollectorProfileFixture(t)

	profile, err := LoadWorkerProfile()
	if err != nil {
		t.Fatalf("load collector worker profile fixture: %v", err)
	}

	return profile
}

func fixtureCollectorConfig(t *testing.T) Config {
	t.Helper()

	loaded, err := workercontract.LoadProfileFile(collectorProfileFixture, WorkerIdentity())
	if err != nil {
		t.Fatal(err)
	}

	profile, err := DecodeWorkerProfile(loaded)
	if err != nil {
		t.Fatal(err)
	}

	return profile.Config()
}
