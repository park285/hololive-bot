// Package settingstest: settings core와 plane 패키지 테스트가 공유하는 fixture·환경 helper다.
// settings를 import 하지 않으므로 core in-package 테스트에서도 순환 없이 쓸 수 있다.
package settingstest

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/park285/shared-go/v2/pkg/workercontract"

	"github.com/kapu/hololive-shared/pkg/config/envload"
	"github.com/kapu/hololive-shared/pkg/config/runtimepolicy"
)

const (
	HololiveH3CertPath = "/run/hololive-bot/certs/hololive-h3.crt"
	HololiveH3KeyPath  = "/run/hololive-bot/certs/hololive-h3.key"

	// 값의 소유자는 settings core의 runtime 입력 로더다. 여기서는 fixture 정리용으로만 쓴다.
	IrisWebhookTokenEnv = "IRIS_WEBHOOK_TOKEN" //nolint:gosec // G101 오탐: 값은 자격증명이 아니라 환경변수 이름이다.
	IrisBotTokenEnv     = "IRIS_BOT_TOKEN"     //nolint:gosec // G101 오탐: 값은 자격증명이 아니라 환경변수 이름이다.
)

// UseProfileFixture는 worker profile 파일 env를 fixture 경로로 세운다. 이 fixture는 identity를 소유한 runtime의
// testdata에 두고, 호출자가 경로를 넘긴다.
func UseProfileFixture(t *testing.T, path string) {
	t.Helper()
	t.Setenv(workercontract.ProfileFileEnv, absFixturePath(t, path))
}

// LoadProfileFixture: fixture를 호출 서비스가 소유한 identity로 읽는다. 둘이 어긋나면 로드가 실패한다.
func LoadProfileFixture(t *testing.T, identity workercontract.Identity, path string) workercontract.LoadedProfile {
	t.Helper()

	loaded, err := workercontract.LoadProfileFile(absFixturePath(t, path), identity)
	if err != nil {
		t.Fatalf("load worker profile fixture: %v", err)
	}

	return loaded
}

// SharedTestProfileFixture는 shared 패키지 시험 전용 profile이다. 여기서는 hololive/api identity를 빌려 registry·metrics
// 조립만 시험하며, hololive-api runtime fixture(internal/config/configtest/testdata)와는 따로 바뀐다.
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

	// 같은 값으로 t.Setenv를 부르는 것은 원래 값 복원 cleanup을 등록하기 위한 것이고, 실제 unset은 그다음 줄이 한다.
	if value, found := os.LookupEnv(key); found {
		t.Setenv(key, value)
	}

	if err := os.Unsetenv(key); err != nil {
		t.Fatalf("os.Unsetenv(%q) error = %v", key, err)
	}
}

// ClearRuntimeRoleEnv: 역할 소유 검증이 호스트 환경에 오염되지 않게 한다.
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

// ClearTracingEnv: OTEL endpoint와 런타임별 토글을 모두 비워 호스트 환경 영향을 없앤다.
func ClearTracingEnv(t *testing.T) {
	t.Helper()

	for _, key := range append([]string{
		envload.OTLPEndpointEnv,
		envload.OTLPTracesEndpointEnv,
		envload.HololiveOTLPGRPCEndpointEnv,
		envload.OTLPInsecureEnv,
		envload.OTELSampleRateEnv,
		"OTEL_ENABLED",
		"OTEL_SERVICE_NAME",
		"OTEL_SERVICE_VERSION",
	}, envload.TracingEnabledEnvKeys()...) {
		t.Setenv(key, "")
	}
}

func SetRuntimeH3ServerEnv(t *testing.T) {
	t.Helper()

	t.Setenv("HOLOLIVE_HTTP_TRANSPORTS", "h3")
	t.Setenv("HOLOLIVE_H3_CERT_FILE", HololiveH3CertPath)
	t.Setenv("HOLOLIVE_H3_KEY_FILE", HololiveH3KeyPath)
}
