package settings

import (
	"strings"
	"testing"
)

// 퇴역 가드의 fail-closed 계약을 고정한다. 제거 조건과 재검토 기한은 config_outbox_v3_handoff_retired_env.go 상단 주석이
// 소유하며, 가드를 삭제하는 리비전에서 이 테스트도 함께 삭제한다.
func TestRejectRetiredOutboxV3HandoffEnvIsPresenceBased(t *testing.T) {
	for _, key := range retiredOutboxV3HandoffEnvKeys {
		for _, value := range []string{"", "off", "cutover"} {
			t.Run(key+"="+value, func(t *testing.T) {
				t.Setenv(key, value)

				err := RejectRetiredOutboxV3HandoffEnv()
				if err == nil || !strings.Contains(err.Error(), key) {
					t.Fatalf("RejectRetiredOutboxV3HandoffEnv() error = %v, want %s rejected on presence", err, key)
				}
			})
		}
	}
}

func TestLoadBotRuntimeRejectsRetiredOutboxV3HandoffMode(t *testing.T) {
	setRequiredLoadEnv(t)
	// 퇴역 전 운영 기본값이던 off도 존재만으로 거절한다.
	t.Setenv("YOUTUBE_OUTBOX_V3_HANDOFF_MODE", "off")

	if _, err := loadBotRuntimeConfig(); err == nil || !strings.Contains(err.Error(), "YOUTUBE_OUTBOX_V3_HANDOFF_MODE is retired") {
		t.Fatalf("loadBotRuntimeConfig() error = %v, want retired YOUTUBE_OUTBOX_V3_HANDOFF_MODE rejection", err)
	}
}
