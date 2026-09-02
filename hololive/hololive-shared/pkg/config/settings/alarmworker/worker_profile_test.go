package alarmworker

import (
	"strings"
	"testing"

	"github.com/kapu/hololive-shared/pkg/config/settings/internal/settingstest"
)

func TestLoadWorkerProfileLoadsExactRoleSettings(t *testing.T) {
	settingstest.UseProfileFixture(t, "stack-worker-profile-alarm-worker.json")

	profile, err := LoadWorkerProfile()
	if err != nil {
		t.Fatalf("LoadWorkerProfile() error = %v", err)
	}

	if _, ok := profile.Loaded.Profile.Workers["alarm_dispatch"]; !ok {
		t.Fatal("alarm worker profile is missing the alarm_dispatch worker")
	}
}

func TestLoadWorkerProfileRejectsWrongRole(t *testing.T) {
	settingstest.UseProfileFixture(t, "stack-worker-profile-api.json")

	if _, err := LoadWorkerProfile(); err == nil || !strings.Contains(err.Error(), "got hololive/api, want hololive/alarm-worker") {
		t.Fatalf("LoadWorkerProfile() error = %v", err)
	}
}
