package config

import (
	"strings"
	"testing"

	"github.com/kapu/hololive-shared/pkg/config/settingstest"
)

// configurePlanes가 읽는 포트·pool env. 잘못된 값은 기본값이나 0으로 바뀌지 않고 기동 실패가 된다(stack audit B4).
var configurePlanesIntEnvKeys = []string{
	"HOLOLIVE_ADMIN_API_PORT",
	"ADMIN_API_POSTGRES_POOL_MIN_CONNS",
	"ADMIN_API_POSTGRES_POOL_MAX_CONNS",
	"LLM_SCHEDULER_PORT",
	"LLM_SCHEDULER_POSTGRES_POOL_MIN_CONNS",
	"LLM_SCHEDULER_POSTGRES_POOL_MAX_CONNS",
	"SERVER_PORT",
	"BOT_POSTGRES_POOL_MIN_CONNS",
	"BOT_POSTGRES_POOL_MAX_CONNS",
}

func clearConfigurePlanesIntEnv(t *testing.T) {
	t.Helper()

	for _, key := range configurePlanesIntEnvKeys {
		t.Setenv(key, "")
	}
}

func TestConfigurePlanesRejectsInvalidEnvValues(t *testing.T) {
	for _, key := range configurePlanesIntEnvKeys {
		t.Run(key, func(t *testing.T) {
			clearConfigurePlanesIntEnv(t)
			t.Setenv(key, "four")

			botConfig := &BotPlaneConfig{}
			adminConfig := &AdminPlaneConfig{}
			llmConfig := &LLMSchedulerConfig{}

			err := configurePlanes(botConfig, adminConfig, llmConfig)
			if err == nil {
				t.Fatalf("configurePlanes() accepted invalid %s; want a startup error", key)
			}

			if !strings.Contains(err.Error(), key) {
				t.Fatalf("configurePlanes() error = %v, want it to name %s", err, key)
			}

			// 파싱이 실패한 포트로 loopback URL을 조립하면 안 된다.
			if adminConfig.BotInternalURL != "" || botConfig.LLMSchedulerURL != "" || adminConfig.LLMSchedulerURL != "" {
				t.Fatalf("configurePlanes() built loopback URLs after a parse error: bot internal %q, llm %q/%q",
					adminConfig.BotInternalURL, botConfig.LLMSchedulerURL, adminConfig.LLMSchedulerURL)
			}
		})
	}
}

// configurePlanes에서만 읽는 키는 LoadRuntime 기동 오류로 전파된다. SERVER_PORT와 LLM_SCHEDULER_PORT는
// bot·llm plane 로더가 먼저 읽어 거절하므로 이 경로의 대상이 아니다.
func TestLoadRuntimeRejectsInvalidPlaneEnvValues(t *testing.T) {
	for _, key := range []string{
		"HOLOLIVE_ADMIN_API_PORT",
		"ADMIN_API_POSTGRES_POOL_MIN_CONNS",
		"ADMIN_API_POSTGRES_POOL_MAX_CONNS",
		"LLM_SCHEDULER_POSTGRES_POOL_MIN_CONNS",
		"LLM_SCHEDULER_POSTGRES_POOL_MAX_CONNS",
		"BOT_POSTGRES_POOL_MIN_CONNS",
		"BOT_POSTGRES_POOL_MAX_CONNS",
	} {
		t.Run(key, func(t *testing.T) {
			settingstest.ClearRuntimeRoleEnv(t)
			settingstest.ClearTracingEnv(t)
			settingstest.SetRequiredLoadEnv(t)
			clearConfigurePlanesIntEnv(t)
			t.Setenv("APP_ENV", "development")
			t.Setenv("ALARM_INTERNAL_URL", "http://127.0.0.1:30007")
			t.Setenv(key, "1.5")

			_, err := LoadRuntime()
			if err == nil {
				t.Fatalf("LoadRuntime() accepted invalid %s; want a startup error", key)
			}

			if !strings.Contains(err.Error(), "configure hololive-api planes") || !strings.Contains(err.Error(), key) {
				t.Fatalf("LoadRuntime() error = %v, want the configure-planes error naming %s", err, key)
			}
		})
	}
}
