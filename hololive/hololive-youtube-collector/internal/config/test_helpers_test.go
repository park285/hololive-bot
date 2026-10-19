package config

import (
	"testing"

	"github.com/park285/shared-go/v2/pkg/workercontract"
)

// collectorProfileFixture는 collector 소유의 fixture다. 테스트가 패키지 디렉터리에서 실행되므로 상대 경로로 둔다.
const collectorProfileFixture = "testdata/stack-worker-profile-youtube-collector.json"

const collectorInstanceC = "youtube-collector-c"

func useCollectorProfileFixture(t *testing.T) {
	t.Helper()
	t.Setenv(workercontract.ProfileFileEnv, collectorProfileFixture)
}

func mustLoadCollectorWorkerProfile(t *testing.T) *WorkerProfile {
	t.Helper()
	useCollectorProfileFixture(t)

	profile, err := LoadWorkerProfile()
	if err != nil {
		t.Fatalf("load YouTube collector worker profile fixture: %v", err)
	}

	return profile
}

func fixtureCollectorConfig(t *testing.T) Config {
	t.Helper()

	identity, err := workercontract.KnownIdentity("hololive", "youtube-collector")
	if err != nil {
		t.Fatal(err)
	}

	loaded, err := workercontract.LoadProfileFile(collectorProfileFixture, identity)
	if err != nil {
		t.Fatal(err)
	}

	profile, err := DecodeWorkerProfile(loaded)
	if err != nil {
		t.Fatal(err)
	}

	return profile.Config()
}
