package settingstest

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/park285/shared-go/v2/pkg/workercontract"

	"github.com/kapu/hololive-shared/pkg/config/runtimepolicy"
)

const (
	HololiveH3CertPath = "/run/hololive-bot/certs/hololive-h3.crt"
	HololiveH3KeyPath  = "/run/hololive-bot/certs/hololive-h3.key"

	IrisWebhookTokenEnv = "IRIS_WEBHOOK_TOKEN" // #nosec G101 -- 값은 자격증명이 아니라 환경변수 이름이다.
	IrisBotTokenEnv     = "IRIS_BOT_TOKEN"     // #nosec G101 -- 값은 자격증명이 아니라 환경변수 이름이다.
)

func UseProfileFixture(t *testing.T, path string) {
	t.Helper()
	t.Setenv(workercontract.ProfileFileEnv, absFixturePath(t, path))
}

func LoadProfileFixture(t *testing.T, identity workercontract.Identity, path string) workercontract.LoadedProfile {
	t.Helper()

	loaded, err := workercontract.LoadProfileFile(absFixturePath(t, path), identity)
	if err != nil {
		t.Fatalf("load worker profile fixture: %v", err)
	}

	return loaded
}

func SharedTestProfileFixture(t *testing.T) string {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve settingstest source path")
	}

	return filepath.Join(filepath.Dir(file), "testdata", "shared-test-worker-profile.json")
}

func absFixturePath(t *testing.T, path string) string {
	t.Helper()

	abs, err := filepath.Abs(path)
	if err != nil {
		t.Fatalf("resolve worker profile fixture %q: %v", path, err)
	}

	return abs
}

func UnsetEnv(t *testing.T, key string) {
	t.Helper()

	if value, found := os.LookupEnv(key); found {
		t.Setenv(key, value)
	}

	if err := os.Unsetenv(key); err != nil {
		t.Fatalf("os.Unsetenv(%q) error = %v", key, err)
	}
}

func ClearRuntimeRoleEnv(t *testing.T) {
	t.Helper()

	for _, key := range []string{
		runtimepolicy.NotificationEgressRoleEnv,
		runtimepolicy.NotificationSchedulerRoleEnv,
	} {
		UnsetEnv(t, key)
	}
}

func ClearIrisAndRoomEnv(t *testing.T) {
	t.Helper()
	ClearRuntimeRoleEnv(t)

	for _, key := range []string{
		IrisWebhookTokenEnv,
		IrisBotTokenEnv,
		"IRIS_BASE_URL",
		"IRIS_BASE_URL_FILE",
		"KAKAO_ROOMS",
	} {
		t.Setenv(key, "")
	}
}

func SetRuntimeH3ServerEnv(t *testing.T) {
	t.Helper()

	t.Setenv("HOLOLIVE_HTTP_TRANSPORTS", "h3")
	t.Setenv("HOLOLIVE_H3_CERT_FILE", HololiveH3CertPath)
	t.Setenv("HOLOLIVE_H3_KEY_FILE", HololiveH3KeyPath)
}
