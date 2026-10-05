package config

import (
	"testing"

	"github.com/kapu/hololive-shared/pkg/config/settingstest"
)

func mustLoadAPIWorkerProfile(t *testing.T) *APIWorkerProfile {
	t.Helper()
	settingstest.UseProfileFixture(t, "stack-worker-profile-api.json")

	profile, err := LoadAPIWorkerProfile()
	if err != nil {
		t.Fatalf("load API worker profile fixture: %v", err)
	}

	return profile
}
