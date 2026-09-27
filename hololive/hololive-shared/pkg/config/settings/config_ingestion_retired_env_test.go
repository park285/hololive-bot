package settings

import (
	"strings"
	"testing"
)

// 퇴역 가드의 fail-closed 계약을 고정한다. 제거 조건과 재검토 기한은 config_ingestion_retired_env.go 상단 주석이
// 소유하며, 가드를 삭제하는 리비전에서 이 테스트도 함께 삭제한다.
func TestRejectRetiredIngestionEnvIsPresenceBased(t *testing.T) {
	for _, key := range retiredIngestionEnvKeys {
		t.Run(key, func(t *testing.T) {
			t.Setenv(key, "")

			if err := rejectRetiredIngestionEnv(); err == nil || !strings.Contains(err.Error(), key) {
				t.Fatalf("rejectRetiredIngestionEnv() error = %v, want %s rejected on presence with an empty value", err, key)
			}
		})
	}
}

func TestLoadBotRuntimeRejectsRetiredCommunityShortsCutoverEnv(t *testing.T) {
	setRequiredLoadEnv(t)
	t.Setenv("YOUTUBE_COMMUNITY_SHORTS_BIGBANG_CUTOVER_AT", "2026-04-10T01:11:12Z")

	if _, err := loadBotRuntimeConfig(); err == nil || !strings.Contains(err.Error(), "YOUTUBE_COMMUNITY_SHORTS_BIGBANG_CUTOVER_AT is retired") {
		t.Fatalf("loadBotRuntimeConfig() error = %v, want retired YOUTUBE_COMMUNITY_SHORTS_BIGBANG_CUTOVER_AT rejection", err)
	}
}
