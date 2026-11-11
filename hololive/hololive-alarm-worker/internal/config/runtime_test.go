package config

import (
	"os"
	"strings"
	"testing"

	"github.com/kapu/hololive-shared/pkg/config/settingstest"
)

func setRuntimeEnv(t *testing.T) {
	t.Helper()
	settingstest.ClearRuntimeRoleEnv(t)
	setWorkerRequiredEnv(t)
	settingstest.UseProfileFixture(t, "testdata/stack-worker-profile-alarm-worker.json")
	t.Setenv("APP_ENV", "development")
}

func setWorkerRequiredEnv(t *testing.T) {
	t.Helper()
	t.Setenv("HOLODEX_API_KEY", "test-key")
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

// alarm-worker는 Iris로 발송하므로 bot 토큰·base URL 원천과 Holodex API 키를 요구한다.
func TestLoadRuntimeRequiresEgressInputs(t *testing.T) {
	for key, want := range map[string]string{
		settingstest.IrisBotTokenEnv: "IRIS_BOT_TOKEN is required",
		"IRIS_BASE_URL":              "IRIS_BASE_URL or IRIS_BASE_URL_FILE is required",
		"HOLODEX_API_KEY":            "HOLODEX_API_KEY is required",
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

// alarm-worker는 webhook을 받지 않고 room ACL도 소비하지 않으므로 webhook 토큰과 room seed 없이 기동한다.
func TestLoadRuntimeDoesNotRequireWebhookTokenOrRoomSeed(t *testing.T) {
	setRuntimeEnv(t)
	t.Setenv(settingstest.IrisWebhookTokenEnv, "")
	t.Setenv("KAKAO_ROOMS", "")

	if _, err := LoadRuntime(); err != nil {
		t.Fatalf("LoadRuntime() error = %v, want success without IRIS_WEBHOOK_TOKEN and KAKAO_ROOMS", err)
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
