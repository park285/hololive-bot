package settings

import (
	"strings"
	"testing"
)

// 퇴역 가드의 fail-closed 계약을 고정한다. 제거 조건과 재검토 기한은 config_ratelimit_retired_env.go 상단 주석이
// 소유하며, 가드를 삭제하는 리비전에서 이 테스트도 함께 삭제한다.
func TestRejectRetiredRateLimiterInstanceIDEnvIsPresenceBased(t *testing.T) {
	for _, value := range []string{"worker-a", ""} {
		t.Setenv("INSTANCE_ID", value)

		err := rejectRetiredRateLimiterInstanceIDEnv()
		if err == nil || !strings.Contains(err.Error(), "INSTANCE_ID") {
			t.Fatalf("INSTANCE_ID=%q: rejectRetiredRateLimiterInstanceIDEnv() error = %v, want presence rejection", value, err)
		}
	}
}

// 분산 limiter를 끄면 limiter 생성 경로가 실행되지 않는다. 그래도 키가 있으면 LoadConfig runtime 기동이 실패해야
// 존재 기준 거절이 limiter 설정과 무관하게 유지된다.
func TestLoadBotRuntimeRejectsRetiredInstanceIDWithDistributedLimiterDisabled(t *testing.T) {
	setRequiredLoadEnv(t)
	t.Setenv("HOLODEX_DISTRIBUTED_RATELIMIT_ENABLED", "false")
	t.Setenv("INSTANCE_ID", "")

	if _, err := loadBotRuntimeConfig(); err == nil || !strings.Contains(err.Error(), "INSTANCE_ID is retired") {
		t.Fatalf("loadBotRuntimeConfig() error = %v, want retired INSTANCE_ID rejection", err)
	}
}
