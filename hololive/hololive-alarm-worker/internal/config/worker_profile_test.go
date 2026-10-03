package config

import (
	"os"
	"strings"
	"testing"

	"github.com/park285/shared-go/v2/pkg/workercontract"

	"github.com/kapu/hololive-shared/pkg/config/settingstest"
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

func TestLoadWorkerProfileRejectsRetiredNotificationLockTimeout(t *testing.T) {
	raw, err := os.ReadFile(settingstest.ProfileFixture(t, "stack-worker-profile-alarm-worker.json"))
	if err != nil {
		t.Fatal(err)
	}

	const existing = `"max_retries": 3,
        "poll_interval_ms": 30000`

	const retired = `"max_retries": 3,
        "lock_timeout_ms": 300000,
        "poll_interval_ms": 30000`

	if strings.Count(string(raw), existing) != 1 {
		t.Fatal("notification_delivery fixture settings changed")
	}

	profileFile, err := os.CreateTemp(t.TempDir(), "alarm-worker-*.json")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := profileFile.WriteString(strings.Replace(string(raw), existing, retired, 1)); err != nil {
		t.Fatal(err)
	}

	if err := profileFile.Close(); err != nil {
		t.Fatal(err)
	}

	t.Setenv(workercontract.ProfileFileEnv, profileFile.Name())

	if _, err := LoadWorkerProfile(); err == nil || !strings.Contains(err.Error(), "notification_delivery") || !strings.Contains(err.Error(), "lock_timeout_ms") {
		t.Fatalf("LoadWorkerProfile() error = %v, want retired notification_delivery.lock_timeout_ms rejected", err)
	}
}

func TestValidateWorkerProfileRequiresYouTubeLockTimeout(t *testing.T) {
	settingstest.UseProfileFixture(t, "stack-worker-profile-alarm-worker.json")

	profile, err := LoadWorkerProfile()
	if err != nil {
		t.Fatalf("LoadWorkerProfile() error = %v", err)
	}

	profile.YouTubeDelivery.LockTimeoutMS = 0

	if err := validateWorkerProfile(profile); err == nil || !strings.Contains(err.Error(), "youtube_delivery.lock_timeout_ms") {
		t.Fatalf("validateWorkerProfile() error = %v, want youtube_delivery lock timeout still required", err)
	}
}

func TestValidateWorkerProfileRequiresMatchingYouTubeAttemptTimeout(t *testing.T) {
	settingstest.UseProfileFixture(t, "stack-worker-profile-alarm-worker.json")

	profile, err := LoadWorkerProfile()
	if err != nil {
		t.Fatalf("LoadWorkerProfile() error = %v", err)
	}

	profile.YouTubeDelivery.DeliverySendTimeoutMS++

	if err := validateWorkerProfile(profile); err == nil || !strings.Contains(err.Error(), "youtube_delivery attempt_timeout must match delivery_send_timeout_ms") {
		t.Fatalf("validateWorkerProfile() error = %v, want conflicting timeout rejected", err)
	}
}
