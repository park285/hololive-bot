package collector

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/park285/shared-go/v2/pkg/workercontract"

	"github.com/kapu/hololive-shared/pkg/config/settings/internal/settingstest"
)

func TestLoadWorkerProfileLoadsExactRoleSettings(t *testing.T) {
	settingstest.UseProfileFixture(t, "stack-worker-profile-youtube-collector.json")

	profile, err := LoadWorkerProfile()
	if err != nil {
		t.Fatalf("LoadWorkerProfile() error = %v", err)
	}

	if _, ok := profile.Loaded.Profile.Workers["collection"]; !ok {
		t.Fatal("youtube collector profile is missing the collection worker")
	}
}

func TestLoadWorkerProfileRejectsUnknownServiceSetting(t *testing.T) {
	raw, err := os.ReadFile(settingstest.ProfileFixture(t, "stack-worker-profile-youtube-collector.json"))
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

	t.Setenv(workercontract.ProfileFileEnv, profileFile.Name())

	if _, err := LoadWorkerProfile(); err == nil || !strings.Contains(err.Error(), "unknown") {
		t.Fatalf("LoadWorkerProfile() error = %v", err)
	}
}
