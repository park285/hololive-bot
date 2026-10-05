package config

import (
	"testing"

	"github.com/park285/shared-go/v2/pkg/workercontract"

	"github.com/kapu/hololive-shared/pkg/config/settingstest"
)

func TestStackWorkerProfileLoadsExactAPIRoleSettings(t *testing.T) {
	settingstest.UseProfileFixture(t, "stack-worker-profile-api.json")

	if _, err := LoadAPIWorkerProfile(); err != nil {
		t.Fatalf("LoadAPIWorkerProfile() error = %v", err)
	}
}

func TestStackWorkerProfileIsRequired(t *testing.T) {
	settingstest.UnsetEnv(t, workercontract.ProfileFileEnv)

	if _, err := LoadAPIWorkerProfile(); err == nil || err.Error() != "load stack worker profile: STACK_WORKER_PROFILE_FILE is required" {
		t.Fatalf("LoadAPIWorkerProfile() error = %v", err)
	}
}
