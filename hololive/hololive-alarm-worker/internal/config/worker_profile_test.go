package config

import (
	jsonv2 "encoding/json/v2"
	"os"
	"path/filepath"
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

func TestValidateWorkerProfileAlarmAttemptLeaseBoundary(t *testing.T) {
	settingstest.UseProfileFixture(t, "stack-worker-profile-alarm-worker.json")

	profile, err := LoadWorkerProfile()
	if err != nil {
		t.Fatal(err)
	}

	worker := profile.Loaded.Profile.Workers[WorkerAlarmDispatch]

	worker.Executor.AttemptTimeout.Milliseconds = new(profile.AlarmDispatch.LeaseMS)
	profile.Loaded.Profile.Workers[WorkerAlarmDispatch] = worker

	if err := validateWorkerProfile(profile); err == nil {
		t.Fatal("attempt equal to lease must be rejected")
	}

	worker.Executor.AttemptTimeout.Milliseconds = new(profile.AlarmDispatch.LeaseMS - 1)
	profile.Loaded.Profile.Workers[WorkerAlarmDispatch] = worker

	if err := validateWorkerProfile(profile); err != nil {
		t.Fatal(err)
	}
}

func TestWorkerProfileDeclaresDomainAgePolicies(t *testing.T) {
	settingstest.UseProfileFixture(t, "stack-worker-profile-alarm-worker.json")

	profile, err := LoadWorkerProfile()
	if err != nil {
		t.Fatal(err)
	}

	for _, id := range []string{WorkerAlarmDispatch, WorkerNotificationDelivery} {
		if got := profile.Loaded.Profile.Workers[id].Queue.MaxAge; got.Mode != workercontract.DurationModeNone || got.Milliseconds != nil {
			t.Fatalf("%s max_age = %+v, want none", id, got)
		}
	}

	worker := profile.Loaded.Profile.Workers[WorkerYouTubeDelivery]
	if got := worker.Queue.MaxAge.Milliseconds; got == nil || *got != 7200000 {
		t.Fatalf("youtube max_age = %v, want existing two-hour parent freshness", got)
	}

	boundary := profile.YouTubeDelivery.ReviveFreshnessWindowMS + profile.YouTubeDelivery.ReviveIntervalMS

	worker.Queue.MaxAge.Milliseconds = new(boundary)
	profile.Loaded.Profile.Workers[WorkerYouTubeDelivery] = worker

	if err := validateWorkerProfile(profile); err != nil {
		t.Fatal(err)
	}

	worker.Queue.MaxAge.Milliseconds = new(boundary - 1)
	profile.Loaded.Profile.Workers[WorkerYouTubeDelivery] = worker

	if err := validateWorkerProfile(profile); err == nil {
		t.Fatal("max_age shorter than revive freshness plus interval must fail")
	}
}

func TestLoadWorkerProfileNotificationDeliveryTimingBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name      string
		attemptMS int64
		staleMS   int64
		valid     bool
	}{
		{name: "audit attempt and stale", attemptMS: 60000, staleMS: 1000},
		{name: "zero attempt", attemptMS: 0, staleMS: 60000},
		{name: "minimum attempt", attemptMS: 1, staleMS: 60000, valid: true},
		{name: "below finalization boundary", attemptMS: 54999, staleMS: 60000, valid: true},
		{name: "at finalization boundary", attemptMS: 55000, staleMS: 60000},
		{name: "above finalization boundary", attemptMS: 55001, staleMS: 60000},
		{name: "stale below sending lease", attemptMS: 10000, staleMS: 59999},
		{name: "stale at sending lease", attemptMS: 10000, staleMS: 60000, valid: true},
		{name: "stale above sending lease", attemptMS: 10000, staleMS: 60001, valid: true},
		{name: "stale covers attempt but not lease", attemptMS: 10000, staleMS: 15000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, enabled := range []bool{true, false} {
				loaded := settingstest.LoadProfileFixture(t, "hololive", "alarm-worker", "stack-worker-profile-alarm-worker.json")

				var settings NotificationDeliveryWorkerSettings

				if err := workercontract.DecodeWorkerSettings(loaded, "notification_delivery", &settings); err != nil {
					t.Fatal(err)
				}

				settings.StaleSendingAfterMS = tc.staleMS

				contents, err := jsonv2.Marshal(settings)
				if err != nil {
					t.Fatal(err)
				}

				worker := loaded.Profile.Workers["notification_delivery"]

				worker.Executor.Enabled = enabled
				worker.Executor.AttemptTimeout.Milliseconds = new(tc.attemptMS)
				worker.Settings = contents
				loaded.Profile.Workers["notification_delivery"] = worker

				contents, err = jsonv2.Marshal(loaded.Profile)
				if err != nil {
					t.Fatal(err)
				}

				path := filepath.Join(t.TempDir(), "notification-delivery.json")
				if writeErr := os.WriteFile(path, contents, 0o600); writeErr != nil {
					t.Fatal(writeErr)
				}

				t.Setenv(workercontract.ProfileFileEnv, path)

				_, err = LoadWorkerProfile()
				if (err == nil) != tc.valid {
					t.Fatalf("LoadWorkerProfile() enabled=%v error=%v, want valid=%v", enabled, err, tc.valid)
				}
			}
		})
	}
}
