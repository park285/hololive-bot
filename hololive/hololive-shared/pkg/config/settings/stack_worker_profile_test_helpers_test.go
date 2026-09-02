package settings

import (
	"testing"

	"github.com/kapu/hololive-shared/pkg/config/settings/internal/settingstest"
)

func useStackWorkerProfileFixture(t *testing.T, name string) {
	t.Helper()
	settingstest.UseProfileFixture(t, name)
}

func apiWorkerProfileFixture(t *testing.T) *APIWorkerProfile {
	t.Helper()

	return &APIWorkerProfile{Loaded: settingstest.LoadProfileFixture(t, "hololive", "api", "stack-worker-profile-api.json")}
}

func alarmWorkerProfileFixture(t *testing.T) *AlarmWorkerProfile {
	t.Helper()

	return &AlarmWorkerProfile{Loaded: settingstest.LoadProfileFixture(t, "hololive", "alarm-worker", "stack-worker-profile-alarm-worker.json")}
}
