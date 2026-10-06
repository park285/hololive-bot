package settings

import (
	"strings"
	"testing"
)

// invalidBoolValue는 shared-go bool 수용 집합(1/0, true/false, yes/no, y/n, on/off)에 없는 값이다.
const invalidBoolValue = "maybe"

// 숫자·bool env의 잘못된 값은 기본값으로 바뀌지 않고 기동 실패로 드러나야 한다(stack audit B4,
// PLN-20260926-stack-audit-refactoring T10). 각 행은 공통 형식 검사가 다시 쓰는 공유 parser 하나를 대표하며,
// 해당 구획을 보관하지 않는 runtime에서도 같은 거절이 유지된다.
func TestValidateRuntimeEnvSyntaxRejectsInvalidEnvValues(t *testing.T) {
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
			t.Setenv(tc.key, tc.value)

			err := ValidateRuntimeEnvSyntax()
			if err == nil {
				t.Fatalf("ValidateRuntimeEnvSyntax() accepted invalid %s=%q; want a startup error", tc.key, tc.value)
			}

			if !strings.Contains(err.Error(), tc.key) {
				t.Fatalf("ValidateRuntimeEnvSyntax() error = %v, want it to name %s", err, tc.key)
			}
		})
	}
}

func TestLoadBotConfigAggregatesFoldAndOtherEnvErrors(t *testing.T) {
	for _, key := range []string{"BOT_SEE_MORE_FOLD", "BOT_MARKDOWN_REPLIES", "BOT_CALENDAR_ENTRY_CACHE_TTL_SECONDS"} {
		t.Setenv(key, "invalid")
	}

	_, err := LoadBotConfig()
	if err == nil {
		t.Fatal("invalid settings accepted")
	}

	for _, key := range []string{"BOT_SEE_MORE_FOLD", "BOT_MARKDOWN_REPLIES", "BOT_CALENDAR_ENTRY_CACHE_TTL_SECONDS"} {
		if !strings.Contains(err.Error(), key) {
			t.Errorf("error %v does not include %s", err, key)
		}
	}
}

func TestLoadIrisConfigRejectsInvalidTimeouts(t *testing.T) {
	for _, value := range []string{"10s", "99999999999999999"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("IRIS_HTTP_TIMEOUT_SECONDS", value)

			if _, err := LoadIrisConfig(); err == nil || !strings.Contains(err.Error(), "IRIS_HTTP_TIMEOUT_SECONDS") {
				t.Fatalf("LoadIrisConfig() error = %v, want IRIS_HTTP_TIMEOUT_SECONDS rejection", err)
			}
		})
	}
}

// 공통 구획 여러 곳의 잘못된 숫자 env는 기본값으로 바뀌지 않고, 한 번의 기동 실패가 잘못된 키를 모두 보인다.
func TestValidateRuntimeEnvSyntaxReportsEveryKey(t *testing.T) {
	t.Setenv("POSTGRES_PORT", "not-a-number")
	t.Setenv("CACHE_PORT", "invalid")
	t.Setenv("SERVER_PORT", "invalid")

	err := ValidateRuntimeEnvSyntax()
	if err == nil {
		t.Fatal("ValidateRuntimeEnvSyntax() error = nil, want invalid numeric env rejection")
	}

	for _, key := range []string{"POSTGRES_PORT", "CACHE_PORT", "SERVER_PORT"} {
		if !strings.Contains(err.Error(), key) {
			t.Fatalf("ValidateRuntimeEnvSyntax() error = %v, want it to name %s", err, key)
		}
	}
}
