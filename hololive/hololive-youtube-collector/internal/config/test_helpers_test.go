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
