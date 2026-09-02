package settings

import (
	"testing"

	"github.com/park285/shared-go/v2/pkg/workercontract"
)

func TestStackWorkerProfileLoadsExactAPIRoleSettings(t *testing.T) {
	useStackWorkerProfileFixture(t, "stack-worker-profile-api.json")

	if _, err := LoadAPIWorkerProfile(); err != nil {
		t.Fatalf("LoadAPIWorkerProfile() error = %v", err)
	}
}

func TestStackWorkerProfileIsRequired(t *testing.T) {
	unsetEnvForTest(t, workercontract.ProfileFileEnv)

	if _, err := LoadAPIWorkerProfile(); err == nil || err.Error() != "load stack worker profile: STACK_WORKER_PROFILE_FILE is required" {
		t.Fatalf("LoadAPIWorkerProfile() error = %v", err)
	}
}
