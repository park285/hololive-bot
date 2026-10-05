package settings

import (
	"strings"
	"testing"
)

// 퇴역 가드의 fail-closed 계약을 고정한다. 제거 조건과 재검토 기한은 config_scraper_retired_env.go 상단 주석이
// 소유하며, 가드를 삭제하는 리비전에서 이 테스트도 함께 삭제한다.
func TestRejectRetiredScraperFetchEnvIsPresenceBased(t *testing.T) {
	for _, key := range retiredScraperFetchEnvKeys {
		t.Run(key, func(t *testing.T) {
			t.Setenv(key, "")

			err := rejectRetiredScraperFetchEnv()
			if err == nil || !strings.Contains(err.Error(), key) {
				t.Fatalf("rejectRetiredScraperFetchEnv() error = %v, want %s rejected on presence with an empty value", err, key)
			}
		})
	}
}

func TestRejectRetiredRuntimeEnvRejectsRetiredScraperFetcherEngine(t *testing.T) {
	// 퇴역 전에 유일하게 허용되던 값도 이제는 존재만으로 거절한다.
	t.Setenv("SCRAPER_FETCHER_ENGINE", "nethttp")

	if err := RejectRetiredRuntimeEnv(); err == nil || !strings.Contains(err.Error(), "SCRAPER_FETCHER_ENGINE is retired") {
		t.Fatalf("RejectRetiredRuntimeEnv() error = %v, want retired SCRAPER_FETCHER_ENGINE rejection", err)
	}
}
