package config

import (
	"os"
	"strings"
	"testing"

	"github.com/kapu/hololive-shared/pkg/config/envload"
	"github.com/kapu/hololive-shared/pkg/config/settingstest"
)

func setRuntimeEnv(t *testing.T) {
	t.Helper()
	settingstest.ClearRuntimeRoleEnv(t)
	setWorkerRequiredEnv(t)
	settingstest.UseProfileFixture(t, "testdata/stack-worker-profile-alarm-worker.json")
	t.Setenv("APP_ENV", "development")
}

// setWorkerRequiredEnv는 alarm-worker LoadRuntime이 요구하는 egress·Iris·H3 입력만 세운다.
func setWorkerRequiredEnv(t *testing.T) {
	t.Helper()
	t.Setenv("HOLODEX_API_KEY", "test-key")
	t.Setenv("KAKAO_ROOMS", "test-room")
	t.Setenv(settingstest.IrisWebhookTokenEnv, "test-webhook-token")
	t.Setenv(settingstest.IrisBotTokenEnv, "test-bot-token")
	t.Setenv("IRIS_BASE_URL", "https://iris.example.test")
	t.Setenv("HOLOLIVE_H3_CERT_FILE", settingstest.HololiveH3CertPath)
	t.Setenv("HOLOLIVE_H3_KEY_FILE", settingstest.HololiveH3KeyPath)
}

func TestLoadRuntimeRejectsInvalidDispatchRetentionEnv(t *testing.T) {
	setRuntimeEnv(t)
	t.Setenv("ALARM_DISPATCH_RETENTION_INTERVAL_MS", "0")

	_, err := LoadRuntime()
	if err == nil {
		t.Fatal("LoadRuntime() error = nil, want alarm dispatch retention rejection")
	}

	if !strings.Contains(err.Error(), "load alarm dispatch retention config: ") || !strings.Contains(err.Error(), "ALARM_DISPATCH_RETENTION_INTERVAL_MS") {
		t.Fatalf("LoadRuntime() error = %v, want wrapped alarm dispatch retention rejection", err)
	}
}

func TestLoadRuntimeIgnoresInvalidYouTubeCollectorEnv(t *testing.T) {
	setRuntimeEnv(t)
	t.Setenv("YOUTUBE_COLLECTOR_INSTANCE_ID", "INVALID")

	if _, err := LoadRuntime(); err != nil {
		t.Fatalf("LoadRuntime() error = %v, want success when collector env is invalid", err)
	}
}

func TestLoadRuntimeSelectsAlarmWorkerTracingToggle(t *testing.T) {
	setRuntimeEnv(t)
	settingstest.ClearTracingEnv(t)
	t.Setenv(envload.HololiveOTLPGRPCEndpointEnv, "otel-collector:4317")
	t.Setenv(envload.TracingHololiveAPIEnabledEnv, "not-a-bool")
	t.Setenv(envload.TracingAlarmWorkerEnabledEnv, "true")

	for _, key := range envload.TracingEnabledEnvKeys()[2:] {
		t.Setenv(key, "not-a-bool")
	}

	config, err := LoadRuntime()
	if err != nil {
		t.Fatalf("LoadRuntime() error = %v", err)
	}

	if !config.Tracing.Enabled {
		t.Fatal("TracingConfig.Enabled = false, want true")
	}
}

// alarm-worker가 읽는 bot 표시 구획의 잘못된 값은 기동 실패로 드러난다.
// LLM·Exa·사진 동기화처럼 hololive-api만 읽는 env는 worker가 검사하지 않는다.
func TestLoadRuntimeRejectsInvalidCommonEnv(t *testing.T) {
	for key, value := range map[string]string{
		"BOT_CALENDAR_ENTRY_CACHE_TTL_SECONDS": "1d",
	} {
		t.Run(key, func(t *testing.T) {
			setRuntimeEnv(t)
			t.Setenv(key, value)

			if _, err := LoadRuntime(); err == nil || !strings.Contains(err.Error(), key) {
				t.Fatalf("LoadRuntime() error = %v, want %s rejection", err, key)
			}
		})
	}
}

// alarm-worker는 Iris egress runtime이므로 room ACL seed와 Iris 토큰 입력을 bot plane과 같은 기준으로 요구한다.
func TestLoadRuntimeRequiresEgressInputs(t *testing.T) {
	for key, want := range map[string]string{
		"KAKAO_ROOMS":                    "KAKAO_ROOMS is required",
		settingstest.IrisWebhookTokenEnv: "IRIS_WEBHOOK_TOKEN is required",
		"HOLODEX_API_KEY":                "HOLODEX_API_KEY is required",
	} {
		t.Run(key, func(t *testing.T) {
			setRuntimeEnv(t)
			t.Setenv(key, "")

			if _, err := LoadRuntime(); err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("LoadRuntime() error = %v, want %q", err, want)
			}
		})
	}
}

func TestLoadRuntimeReadsMarkdownReplies(t *testing.T) {
	setRuntimeEnv(t)
	t.Setenv("BOT_MARKDOWN_REPLIES", "true")

	config, err := LoadRuntime()
	if err != nil {
		t.Fatalf("LoadRuntime() error = %v", err)
	}

	if !config.MarkdownReplies {
		t.Fatal("MarkdownReplies = false, want BOT_MARKDOWN_REPLIES=true")
	}

	if config.AlarmWorkerProfile == nil {
		t.Fatal("AlarmWorkerProfile = nil, want loaded Stack Worker Profile v1")
	}
}

func TestLoadRuntimeRequiresControlCapacityAndPreservesIdleZero(t *testing.T) {
	setRuntimeEnv(t)
	t.Setenv("POSTGRES_POOL_MIN_CONNS", "0")
	t.Setenv("POSTGRES_POOL_MAX_CONNS", "1")

	if _, err := LoadRuntime(); err == nil || !strings.Contains(err.Error(), "POSTGRES_POOL_MAX_CONNS") {
		t.Fatalf("LoadRuntime() error = %v, want control reserve rejection", err)
	}

	t.Setenv("POSTGRES_POOL_MAX_CONNS", "2")

	config, err := LoadRuntime()
	if err != nil {
		t.Fatal(err)
	}

	if config.Postgres.PoolMinConns != 0 || config.Postgres.PoolMaxConns != 2 {
		t.Fatalf("pool bounds = %d/%d, want 0/2", config.Postgres.PoolMinConns, config.Postgres.PoolMaxConns)
	}
}

// 발행 batch 한도는 config가 소유한다. 미설정은 1000이고, 잘못된 값은 기본값으로 바뀌지 않고 기동 오류다.
func TestLoadRuntimeAlarmDispatchPublishBatchLimit(t *testing.T) {
	setRuntimeEnv(t)
	t.Setenv("ALARM_DISPATCH_MAX_DELIVERIES_PER_BATCH", "1000")

	if err := os.Unsetenv("ALARM_DISPATCH_MAX_DELIVERIES_PER_BATCH"); err != nil {
		t.Fatal(err)
	}

	config, err := LoadRuntime()
	if err != nil {
		t.Fatalf("LoadRuntime() error = %v", err)
	}

	if got := config.AlarmDispatchPublish.MaxDeliveriesPerBatch; got != 1000 {
		t.Fatalf("MaxDeliveriesPerBatch = %d, want 1000", got)
	}

	for _, value := range []string{"many", "0", "-10", "", "  "} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("ALARM_DISPATCH_MAX_DELIVERIES_PER_BATCH", value)

			_, err := LoadRuntime()
			if err == nil || !strings.Contains(err.Error(), "ALARM_DISPATCH_MAX_DELIVERIES_PER_BATCH") {
				t.Fatalf("LoadRuntime() error = %v, want batch limit rejection", err)
			}
		})
	}
}
