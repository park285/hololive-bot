package settings

import "testing"

// 퇴역 가드의 fail-closed 계약을 고정한다. 제거 조건과 재검토 기한은 config_youtube_retired_env.go 상단 주석이
// 소유하며, 가드를 삭제하는 리비전에서 이 테스트도 함께 삭제한다.
func TestLoadYouTubeConfigRejectsRetiredCacheExpirationEnv(t *testing.T) {
	t.Setenv("YOUTUBE_CACHE_EXPIRATION_SECONDS", "7200")

	if _, err := loadYouTubeConfig(); err == nil {
		t.Fatal("loadYouTubeConfig must reject retired YOUTUBE_CACHE_EXPIRATION_SECONDS")
	}
}

// 퇴역 가드는 값이 비어 있어도 키가 있으면 거절한다(존재 기준).
func TestRejectRetiredYouTubeConfigEnvIsPresenceBased(t *testing.T) {
	for _, key := range retiredYouTubeConfigEnvKeys {
		t.Run(key, func(t *testing.T) {
			t.Setenv(key, "")

			if err := rejectRetiredYouTubeConfigEnv(); err == nil {
				t.Fatalf("retired YouTube config env %s must fail closed on presence", key)
			}
		})
	}
}

// RSS backoff TTL 키는 유효해 보이는 값이어도 거절한다(DEC-20260926-hololive-source-fallbacks-retirement).
func TestLoadYouTubeConfigRejectsRetiredVideoRSSBackoffEnv(t *testing.T) {
	t.Setenv("YOUTUBE_VIDEO_RSS_BACKOFF_TTL_SECONDS", "21600")

	if _, err := loadYouTubeConfig(); err == nil {
		t.Fatal("loadYouTubeConfig must reject retired YOUTUBE_VIDEO_RSS_BACKOFF_TTL_SECONDS")
	}
}
