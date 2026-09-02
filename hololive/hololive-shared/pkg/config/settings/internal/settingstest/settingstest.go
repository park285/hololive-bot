// Package settingstest: settings core와 plane 패키지 테스트가 공유하는 fixture·환경 helper다.
// settings를 import 하지 않으므로 core in-package 테스트에서도 순환 없이 쓸 수 있다.
package settingstest

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/park285/shared-go/v2/pkg/workercontract"

	"github.com/kapu/hololive-shared/pkg/config/settings/internal/load"
)

const (
	CollectorInstanceC = "youtube-collector-c"

	HololiveH3CertPath = "/run/hololive-bot/certs/hololive-h3.crt"
	HololiveH3KeyPath  = "/run/hololive-bot/certs/hololive-h3.key"

	// 값의 소유자는 settings core의 runtime 입력 로더다. 여기서는 fixture 정리용으로만 쓴다.
	IrisWebhookTokenEnv = "IRIS_WEBHOOK_TOKEN" //nolint:gosec // G101 오탐: 값은 자격증명이 아니라 환경변수 이름이다.
	IrisBotTokenEnv     = "IRIS_BOT_TOKEN"     //nolint:gosec // G101 오탐: 값은 자격증명이 아니라 환경변수 이름이다.
)

// ProfileFixture: settings 패키지의 testdata 경로를 하위 패키지에서도 같은 값으로 돌려준다.
func ProfileFixture(t *testing.T, name string) string {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve settingstest source path")
	}

	return filepath.Join(filepath.Dir(file), "..", "..", "testdata", name)
}

func UseProfileFixture(t *testing.T, name string) {
	t.Helper()
	t.Setenv(workercontract.ProfileFileEnv, ProfileFixture(t, name))
}

func LoadProfileFixture(t *testing.T, service, role, name string) workercontract.LoadedProfile {
	t.Helper()

	identity, err := workercontract.KnownIdentity(service, role)
	if err != nil {
		t.Fatalf("resolve worker identity: %v", err)
	}

	loaded, err := workercontract.LoadProfileFile(ProfileFixture(t, name), identity)
	if err != nil {
		t.Fatalf("load worker profile fixture: %v", err)
	}

	return loaded
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
		load.NotificationEgressRoleEnv,
		load.NotificationSchedulerRoleEnv,
		"MEMBER_NEWS_CLIPROXY_MODEL",
		"DB_SSLMODE",
		"DB_QUERY_EXEC_MODE",
		"OTEL_ENVIRONMENT",
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
		load.OTLPEndpointEnv,
		load.OTLPTracesEndpointEnv,
		load.HololiveOTLPGRPCEndpointEnv,
		load.OTLPInsecureEnv,
		load.OTELSampleRateEnv,
		"OTEL_ENABLED",
		"OTEL_SERVICE_NAME",
		"OTEL_SERVICE_VERSION",
	}, load.TracingEnabledEnvKeys()...) {
		t.Setenv(key, "")
	}
}

func SetRuntimeH3ServerEnv(t *testing.T) {
	t.Helper()

	t.Setenv("HOLOLIVE_HTTP_TRANSPORTS", "h3")
	t.Setenv("HOLOLIVE_H3_CERT_FILE", HololiveH3CertPath)
	t.Setenv("HOLOLIVE_H3_KEY_FILE", HololiveH3KeyPath)
}
