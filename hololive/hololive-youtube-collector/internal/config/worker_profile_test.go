package config

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/park285/shared-go/v2/pkg/workercontract"
)

func TestLoadWorkerProfileLoadsExactRoleSettings(t *testing.T) {
	useCollectorProfileFixture(t)

	profile, err := LoadWorkerProfile()
	if err != nil {
		t.Fatalf("LoadWorkerProfile() error = %v", err)
	}

	if _, ok := profile.Loaded.Profile.Workers["collection"]; !ok {
		t.Fatal("youtube collector profile is missing the collection worker")
	}
}

func TestLoadWorkerProfileRejectsUnknownServiceSetting(t *testing.T) {
	useMutatedCollectorProfile(t, `"youtubejs_max_inflight": 4`, `"youtubejs_max_inflight": 4, "unknown_setting": 1`)

	if _, err := LoadWorkerProfile(); err == nil || !strings.Contains(err.Error(), "unknown") {
		t.Fatalf("LoadWorkerProfile() error = %v", err)
	}
}

// --check-worker-profile은 LoadWorkerProfile만 호출하므로, 런타임 Config.Validate가 거절할 profile 값은
// DB·provider env 없이 이 단계에서 먼저 거절되어야 한다.
func TestLoadWorkerProfileRejectsRuntimeInvalidPolicyWithoutRuntimeEnv(t *testing.T) {
	for _, test := range []struct {
		name             string
		old, replacement string
		wantSub          string
	}{
		{
			name: "lease ttl above 30m", old: `"lease_ttl_ms": 60000`, replacement: `"lease_ttl_ms": 3600000`,
			wantSub: "lease_ttl_ms must be between 1000 and 1800000",
		},
		{
			name: "db timeout above 1m", old: `"db_timeout_ms": 5000`, replacement: `"db_timeout_ms": 120000`,
			wantSub: "phase timeout bounds are invalid",
		},
		{
			name: "acquisition cadence above 1m", old: `"acquisition_cadence_ms": 1000`, replacement: `"acquisition_cadence_ms": 120000`,
			wantSub: "acquisition_cadence_ms must be between 100 and 60000",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			useMutatedCollectorProfile(t, test.old, test.replacement)

			if _, err := LoadWorkerProfile(); err == nil || !strings.Contains(err.Error(), test.wantSub) {
				t.Fatalf("LoadWorkerProfile() error = %v, want %q", err, test.wantSub)
			}
		})
	}
}

func TestLoadRuntimeAppliesProfileQueueMaxAge(t *testing.T) {
	setYouTubeCollectorRuntimeLoadEnv(t)
	useMutatedCollectorProfile(t, `"milliseconds": 3600000`, `"milliseconds": 90000`)

	cfg, err := LoadRuntime()
	if err != nil {
		t.Fatalf("LoadRuntime() error = %v", err)
	}

	if cfg.Collector.QueueMaxAge != 90*time.Second {
		t.Fatalf("QueueMaxAge = %s, want profile max_age 1m30s", cfg.Collector.QueueMaxAge)
	}
}

func useMutatedCollectorProfile(t *testing.T, old, replacement string) {
	t.Helper()

	raw, err := os.ReadFile(collectorProfileFixture)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(raw), old) {
		t.Fatalf("profile fixture does not contain %s", old)
	}

	mutated := strings.Replace(string(raw), old, replacement, 1)

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

	t.Setenv(workercontract.ProfileFileEnv, profileFile.Name())
}
