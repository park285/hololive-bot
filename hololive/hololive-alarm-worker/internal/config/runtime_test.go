package config

import (
	"strings"
	"testing"

	"github.com/kapu/hololive-shared/pkg/config/envload"
	"github.com/kapu/hololive-shared/pkg/config/settingstest"
)

func setRuntimeEnv(t *testing.T) {
	t.Helper()
	settingstest.ClearRuntimeRoleEnv(t)
	settingstest.SetRequiredLoadEnv(t)
	settingstest.UseProfileFixture(t, "stack-worker-profile-alarm-worker.json")
	t.Setenv("APP_ENV", "development")
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

// alarm-worker가 보관하지 않는 공통 구획(LLM·bot 표시·CORS 등)의 잘못된 값도 예전처럼 기동 실패로 드러난다.
func TestLoadRuntimeRejectsInvalidUnconsumedCommonEnv(t *testing.T) {
	for key, value := range map[string]string{
		"MEMBER_NEWS_TEMPERATURE":              "warm",
		"BOT_CALENDAR_ENTRY_CACHE_TTL_SECONDS": "1d",
		"CORS_ENFORCE":                         "maybe",
		"EXA_ENABLED":                          "maybe",
		"PHOTO_SYNC_ENABLED":                   "maybe",
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
