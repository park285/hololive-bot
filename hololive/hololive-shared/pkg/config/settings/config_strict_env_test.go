package settings

import (
	"strings"
	"testing"
)

// invalidBoolValue는 shared-go bool 수용 집합(1/0, true/false, yes/no, y/n, on/off)에 없는 값이다.
const invalidBoolValue = "maybe"

// 숫자·bool env의 잘못된 값은 기본값으로 바뀌지 않고 기동 실패로 드러나야 한다(stack audit B4,
// PLN-20260926-stack-audit-refactoring T10). 각 행은 LoadConfig가 조립하는 로더 하나를 대표한다.
func TestLoadBotRuntimeRejectsInvalidEnvValues(t *testing.T) {
	for _, tc := range []struct {
		key   string
		value string
	}{
		{key: "POSTGRES_PORT", value: "not-a-number"},
		{key: "POSTGRES_POOL_MAX_CONNS", value: "four"},
		{key: "CACHE_PORT", value: "invalid"},
		{key: "CACHE_DB", value: "zero"},
		{key: "SERVER_PORT", value: "invalid"},
		{key: "AUTH_BCRYPT_COST", value: "high"},
		{key: "LOG_MAX_SIZE_MB", value: "5MB"},
		{key: "LOG_COMPRESS", value: invalidBoolValue},
		{key: "BOT_CALENDAR_ENTRY_CACHE_TTL_SECONDS", value: "1d"},
		{key: "BOT_SEE_MORE_FOLD", value: invalidBoolValue},
		{key: "BOT_MARKDOWN_REPLIES", value: invalidBoolValue},
		{key: "IRIS_HTTP_TIMEOUT_SECONDS", value: "10s"},
		{key: "IRIS_HTTP_TIMEOUT_SECONDS", value: "99999999999999999"},
		{key: "HOLODEX_MAX_RETRY_ATTEMPTS", value: "three"},
		{key: "HOLODEX_REQUEST_DELAY_MS", value: "500ms"},
		{key: "HOLODEX_DISTRIBUTED_RATELIMIT_ENABLED", value: invalidBoolValue},
		{key: "OFFICIAL_SCHEDULE_PAGE_CACHE_TTL_SECONDS", value: "15s"},
		{key: "CHECK_INTERVAL_SECONDS", value: "1m"},
		{key: "CLIPROXY_ENABLED", value: invalidBoolValue},
		{key: "MEMBER_NEWS_TEMPERATURE", value: "warm"},
		{key: "MEMBER_NEWS_CONSENSUS_ENABLED", value: invalidBoolValue},
		{key: "MAJOREVENT_REVIEW_TIMEOUT_SEC", value: "30s"},
		{key: "EXA_ENABLED", value: invalidBoolValue},
		{key: "MAX_RESPONSE_BODY_BYTES", value: "2MB"},
		{key: "PHOTO_SYNC_ENABLED", value: invalidBoolValue},
		{key: "CORS_ENFORCE", value: invalidBoolValue},
	} {
		t.Run(tc.key+"="+tc.value, func(t *testing.T) {
			setRequiredLoadEnv(t)
			t.Setenv(tc.key, tc.value)

			_, err := loadBotRuntimeConfig()
			if err == nil {
				t.Fatalf("Load() accepted invalid %s=%q; want a startup error", tc.key, tc.value)
			}

			if !strings.Contains(err.Error(), tc.key) {
				t.Fatalf("Load() error = %v, want it to name %s", err, tc.key)
			}
		})
	}
}

// 퇴역한 env 이름은 값을 읽지 않고 존재만으로(빈 값 포함) 거절한다. 제거 조건과 재검토 기한은
// pkg/config/envload/retired_env_aliases.go와 config_services_retired_env.go 상단 주석이 소유한다.
func TestLoadBotRuntimeRejectsRetiredEnvAliases(t *testing.T) {
	for _, key := range []string{"HOLODEX_API_KEY_1", "SERVICES_LLM_SERVER_HEALTH_URL"} {
		for _, value := range []string{"", "legacy-value"} {
			t.Run(key+"="+value, func(t *testing.T) {
				setRequiredLoadEnv(t)
				t.Setenv(key, value)

				_, err := loadBotRuntimeConfig()
				if err == nil {
					t.Fatalf("Load() accepted retired %s=%q; want presence-based rejection", key, value)
				}

				if !strings.Contains(err.Error(), key) {
					t.Fatalf("Load() error = %v, want it to name %s", err, key)
				}
			})
		}
	}
}

// SERVICES_LLM_SCHEDULER_HEALTH_URL 하나만 정본이다.
func TestLoadBotRuntimeReadsCanonicalLLMSchedulerHealthURL(t *testing.T) {
	setRequiredLoadEnv(t)
	t.Setenv("SERVICES_LLM_SCHEDULER_HEALTH_URL", " https://llm.internal/health ")

	config, err := loadBotRuntimeConfig()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if config.Services.LLMSchedulerHealthURL != "https://llm.internal/health" {
		t.Fatalf("Services.LLMSchedulerHealthURL = %q, want canonical value", config.Services.LLMSchedulerHealthURL)
	}
}

func TestLoadBotConfigAggregatesFoldAndOtherEnvErrors(t *testing.T) {
	for _, key := range []string{"BOT_SEE_MORE_FOLD", "BOT_MARKDOWN_REPLIES", "BOT_CALENDAR_ENTRY_CACHE_TTL_SECONDS"} {
		t.Setenv(key, "invalid")
	}

	_, err := loadBotConfig()
	if err == nil {
		t.Fatal("invalid settings accepted")
	}

	for _, key := range []string{"BOT_SEE_MORE_FOLD", "BOT_MARKDOWN_REPLIES", "BOT_CALENDAR_ENTRY_CACHE_TTL_SECONDS"} {
		if !strings.Contains(err.Error(), key) {
			t.Errorf("error %v does not include %s", err, key)
		}
	}
}
