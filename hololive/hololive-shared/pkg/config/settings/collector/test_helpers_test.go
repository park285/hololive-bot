package collector

import (
	"testing"

	"github.com/kapu/hololive-shared/pkg/config/settings"
	"github.com/kapu/hololive-shared/pkg/config/settings/internal/settingstest"
)

func mustLoadCollectorWorkerProfile(t *testing.T) *settings.YouTubeCollectorWorkerProfile {
	t.Helper()
	settingstest.UseProfileFixture(t, "stack-worker-profile-youtube-collector.json")

	profile, err := LoadWorkerProfile()
	if err != nil {
		t.Fatalf("load YouTube collector worker profile fixture: %v", err)
	}

	return profile
}
