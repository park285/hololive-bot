package settings

import (
	"strings"
	"testing"
)

// 퇴역 가드의 fail-closed 계약을 고정한다. 제거 조건과 재검토 기한은 config_official_schedule_retired_env.go 상단
// 주석이 소유하며, 가드를 삭제하는 리비전에서 이 테스트도 함께 삭제한다.
func TestLoadOfficialScheduleConfigRejectsRetiredCacheExpiryEnv(t *testing.T) {
	for _, value := range []string{"", "1800"} {
		t.Run("value="+value, func(t *testing.T) {
			t.Setenv("OFFICIAL_SCHEDULE_CACHE_EXPIRY_SECONDS", value)

			_, err := LoadOfficialScheduleRuntimeConfig()
			if err == nil {
				t.Fatalf("LoadOfficialScheduleRuntimeConfig accepted retired OFFICIAL_SCHEDULE_CACHE_EXPIRY_SECONDS=%q", value)
			}

			if !strings.Contains(err.Error(), "OFFICIAL_SCHEDULE_CACHE_EXPIRY_SECONDS") {
				t.Fatalf("error = %v, want it to name the retired key", err)
			}
		})
	}
}
