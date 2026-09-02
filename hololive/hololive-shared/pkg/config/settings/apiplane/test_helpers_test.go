package apiplane

import (
	"testing"

	"github.com/kapu/hololive-shared/pkg/config/settings"
	"github.com/kapu/hololive-shared/pkg/config/settings/internal/settingstest"
)

func mustLoadAPIWorkerProfile(t *testing.T) *settings.APIWorkerProfile {
	t.Helper()
	settingstest.UseProfileFixture(t, "stack-worker-profile-api.json")

	profile, err := settings.LoadAPIWorkerProfile()
	if err != nil {
		t.Fatalf("load API worker profile fixture: %v", err)
	}

	return profile
}
