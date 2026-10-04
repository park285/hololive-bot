package config

import (
	"strings"
	"testing"
)

// 퇴역 가드의 fail-closed 계약을 고정한다. 제거 조건과 재검토 기한은 retired_env.go 상단 주석이 소유하며,
// 가드를 삭제하는 리비전에서 이 테스트도 함께 삭제한다.
func TestLoadRuntimeRejectsRetiredCollectorEnvOnPresence(t *testing.T) {
	for _, key := range []string{
		"SCRAPER_PROXY_ENABLED",
		"SCRAPER_PROXY_URL",
		"YOUTUBE_COLLECTOR_MAX_AGGREGATE_BYTES",
		"YOUTUBE_COLLECTOR_YOUTUBEJS_TIMEOUT_SECONDS",
	} {
		t.Run(key, func(t *testing.T) {
			setYouTubeCollectorRuntimeLoadEnv(t)
			// T18(2026-09-26)에서 youtube-collector.env에 빈 값으로 남아 있던 형태도 존재만으로 거절한다.
			t.Setenv(key, "")

			if _, err := LoadRuntime(); err == nil || !strings.Contains(err.Error(), key+" is retired") {
				t.Fatalf("LoadRuntime() error = %v, want retired %s rejection", err, key)
			}
		})
	}
}
