package settings

import "testing"

// 퇴역 가드의 fail-closed 계약을 고정한다. 제거 조건과 재검토 기한은 config_holodex_retired_env.go 상단 주석이
// 소유하며, 가드를 삭제하는 리비전에서 이 테스트도 함께 삭제한다.
func TestRejectRetiredHolodexLiveStatusFallbackEnvIsPresenceBased(t *testing.T) {
	for _, key := range retiredHolodexLiveStatusFallbackEnvKeys {
		t.Run(key, func(t *testing.T) {
			t.Setenv(key, "")

			if err := rejectRetiredHolodexLiveStatusFallbackEnv(); err == nil {
				t.Fatalf("%s must fail closed on presence even with an empty value", key)
			}
		})
	}
}

func TestLoadBotRuntimeRejectsRetiredHolodexLiveStatusFallbackEnv(t *testing.T) {
	setRequiredLoadEnv(t)
	t.Setenv("HOLODEX_LIVE_STATUS_FALLBACK_MAX_PER_CYCLE", "4")

	if _, err := loadBotRuntimeConfig(); err == nil {
		t.Fatal("loadBotRuntimeConfig must reject retired HOLODEX_LIVE_STATUS_FALLBACK_MAX_PER_CYCLE")
	}
}

func TestLoadBotRuntimeRejectsRetiredLLMTokenCeilingEnv(t *testing.T) {
	setRequiredLoadEnv(t)
	t.Setenv("LLM_MONTHLY_TOKEN_CEILING", "0")

	if _, err := loadBotRuntimeConfig(); err == nil {
		t.Fatal("loadBotRuntimeConfig must reject retired LLM_MONTHLY_TOKEN_CEILING")
	}
}
