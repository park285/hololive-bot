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

// notification_delivery.lock_timeout_ms는 lease 이전 행 회수에만 쓰였고 그 경로를 지운 뒤 읽는 코드가 없다. 키는 exact-key
// profile 계약 때문에 남지만 값은 기동을 막지 않는다(stack-audit 2026-09-26 T11 holo-delivery-outbox-legacy-lock-fence).
func TestValidateWorkerProfileIgnoresRetiredNotificationLockTimeout(t *testing.T) {
	settingstest.UseProfileFixture(t, "stack-worker-profile-alarm-worker.json")

	profile, err := LoadWorkerProfile()
	if err != nil {
		t.Fatalf("LoadWorkerProfile() error = %v", err)
	}

	profile.NotificationDelivery.LockTimeoutMS = 0

	if err := validateWorkerProfile(profile); err != nil {
		t.Fatalf("validateWorkerProfile() error = %v, want retired lock timeout ignored", err)
	}

	profile.YouTubeDelivery.LockTimeoutMS = 0

	if err := validateWorkerProfile(profile); err == nil || !strings.Contains(err.Error(), "youtube_delivery.lock_timeout_ms") {
		t.Fatalf("validateWorkerProfile() error = %v, want youtube_delivery lock timeout still required", err)
	}
}
