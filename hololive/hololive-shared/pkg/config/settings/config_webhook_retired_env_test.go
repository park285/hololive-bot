package settings

import (
	"strings"
	"testing"
)

// 퇴역 가드의 fail-closed 계약을 고정한다. 제거 조건과 재검토 기한은 config_webhook_retired_env.go 상단 주석이 소유하며,
// 가드를 삭제하는 리비전에서 이 테스트도 함께 삭제한다.
func TestRejectRetiredRuntimeEnvRejectsRetiredWebhookRequireHMACEvenWhenTrue(t *testing.T) {
	// 예전에 유일하게 허용하던 값(true)도 존재만으로 거절해야 한다.
	t.Setenv("IRIS_WEBHOOK_REQUIRE_HMAC", "true")

	err := RejectRetiredRuntimeEnv()
	if err == nil || !strings.Contains(err.Error(), "IRIS_WEBHOOK_REQUIRE_HMAC is retired") {
		t.Fatalf("RejectRetiredRuntimeEnv() error = %v, want retired IRIS_WEBHOOK_REQUIRE_HMAC rejection", err)
	}
}

func TestRejectRetiredWebhookEnvIsPresenceBased(t *testing.T) {
	for _, key := range retiredWebhookEnvKeys {
		t.Run(key, func(t *testing.T) {
			t.Setenv(key, "")

			if err := rejectRetiredWebhookEnv(); err == nil || !strings.Contains(err.Error(), key) {
				t.Fatalf("rejectRetiredWebhookEnv() error = %v, want %s rejected on presence with an empty value", err, key)
			}
		})
	}
}
