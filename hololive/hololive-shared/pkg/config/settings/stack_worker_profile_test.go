package settings

import (
	"errors"
	"os"
	"strings"
	"testing"
)

func TestStackWorkerProfilesLoadExactRoleSettings(t *testing.T) {
	tests := []struct {
		name     string
		fixture  string
		load     func() error
		workerID string
	}{
		{
			name: "api", fixture: "stack-worker-profile-api.json", workerID: "bot_webhook_inbox",
			load: func() error { _, err := LoadAPIWorkerProfile(); return err },
		},
		{
			name: "alarm-worker", fixture: "stack-worker-profile-alarm-worker.json", workerID: "alarm_dispatch",
			load: func() error { _, err := LoadAlarmWorkerProfile(); return err },
		},
		{
			name: "youtube-collector", fixture: "stack-worker-profile-youtube-collector.json", workerID: "collection",
			load: func() error { _, err := LoadYouTubeCollectorWorkerProfile(); return err },
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			useStackWorkerProfileFixture(t, test.fixture)
			if err := test.load(); err != nil {
				t.Fatalf("load profile: %v", err)
			}
		})
	}
}

func TestStackWorkerProfileIsRequired(t *testing.T) {
	unsetEnvForTest(t, StackWorkerProfileFileEnv)
	if _, err := LoadAPIWorkerProfile(); err == nil || err.Error() != "STACK_WORKER_PROFILE_FILE is required" {
		t.Fatalf("LoadAPIWorkerProfile() error = %v", err)
	}
}

func TestStackWorkerProfileRejectsWrongRole(t *testing.T) {
	useStackWorkerProfileFixture(t, "stack-worker-profile-api.json")
	if _, err := LoadAlarmWorkerProfile(); err == nil || !strings.Contains(err.Error(), "got hololive/api, want hololive/alarm-worker") {
		t.Fatalf("LoadAlarmWorkerProfile() error = %v", err)
	}
}

func TestStackWorkerProfileRejectsUnknownServiceSetting(t *testing.T) {
	raw, err := os.ReadFile(stackWorkerProfileFixture(t, "stack-worker-profile-youtube-collector.json"))
	if err != nil {
		t.Fatal(err)
	}
	mutated := strings.Replace(string(raw), `"youtubejs_max_inflight": 4`, `"youtubejs_max_inflight": 4, "unknown_setting": 1`, 1)
	profileFile, err := os.CreateTemp(t.TempDir(), "profile-*.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := profileFile.WriteString(mutated); err != nil {
		t.Fatal(errors.Join(err, profileFile.Close()))
	}
	if err := profileFile.Close(); err != nil {
		t.Fatal(err)
	}
	t.Setenv(StackWorkerProfileFileEnv, profileFile.Name())
	if _, err := LoadYouTubeCollectorWorkerProfile(); err == nil || !strings.Contains(err.Error(), "unknown") {
		t.Fatalf("LoadYouTubeCollectorWorkerProfile() error = %v", err)
	}
}
