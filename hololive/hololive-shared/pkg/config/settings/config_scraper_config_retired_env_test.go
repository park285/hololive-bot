package settings

import (
	"strings"
	"testing"
)

// 퇴역 가드의 fail-closed 계약을 고정한다. 제거 조건과 재검토 기한은 config_scraper_config_retired_env.go 상단 주석이
// 소유하며, 가드를 삭제하는 리비전에서 이 테스트도 함께 삭제한다.
func TestRejectRetiredScraperConfigEnvIsPresenceBased(t *testing.T) {
	for _, key := range append(append([]string{}, retiredScraperConfigEnvKeys...), retiredIgnoredLegacyEnvKeys...) {
		t.Run(key, func(t *testing.T) {
			t.Setenv(key, "")

			err := rejectRetiredScraperConfigEnv()
			if err == nil || !strings.Contains(err.Error(), key) {
				t.Fatalf("rejectRetiredScraperConfigEnv() error = %v, want %s rejected on presence with an empty value", err, key)
			}
		})
	}
}

// 이전에는 아래 키를 가드 없이 무시하거나(삭제된 alias) 소비자 없이 파싱했다. 이제는 egress runtime 공통 거절(RejectRetiredRuntimeEnv)이 기동을 막는다.
func TestRejectRetiredRuntimeEnvRejectsRetiredScraperAndIgnoredLegacyEnv(t *testing.T) {
	for _, tc := range []struct{ key, value string }{
		{"SCRAPER_VIDEOS_SECONDS", "420"},
		{"SCRAPER_WORKER_COUNT", "6"},
		{"IRIS_SHARED_TOKEN", "shared-token"},
		{"SCRAPER_SCHEDULER_WORKER_COUNT", "2"},
		{"SCRAPER_POLL_VIDEOS_INTERVAL_SECONDS", "300"},
		{"SCRAPER_BACKFILL_TARGET_GROUP", "notification"},
		{"SCRAPER_PROXY_ENABLED", "false"},
		{"SCRAPER_PROXY_URL", ""},
	} {
		t.Run(tc.key, func(t *testing.T) {
			t.Setenv(tc.key, tc.value)

			if err := RejectRetiredRuntimeEnv(); err == nil || !strings.Contains(err.Error(), tc.key+" is retired") {
				t.Fatalf("RejectRetiredRuntimeEnv() error = %v, want retired %s rejection", err, tc.key)
			}
		})
	}
}
