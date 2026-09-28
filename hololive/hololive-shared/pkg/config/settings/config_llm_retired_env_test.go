package settings

import "testing"

// 퇴역 가드의 fail-closed 계약을 고정한다. 제거 조건과 재검토 기한은 config_llm_retired_env.go 상단 주석이
// 소유하며, 가드를 삭제하는 리비전에서 이 테스트도 함께 삭제한다.
func TestRejectRetiredLLMEnvIsPresenceBased(t *testing.T) {
	t.Setenv("LLM_MONTHLY_TOKEN_CEILING", "")

	if err := RejectRetiredLLMEnv(); err == nil {
		t.Fatal("retired LLM env must fail closed on presence even with an empty value")
	}
}

func TestRejectRetiredLLMEnvRejectsValue(t *testing.T) {
	t.Setenv("LLM_MONTHLY_TOKEN_CEILING", "1000000")

	if err := RejectRetiredLLMEnv(); err == nil {
		t.Fatal("retired LLM env must fail closed when a value is set")
	}
}
