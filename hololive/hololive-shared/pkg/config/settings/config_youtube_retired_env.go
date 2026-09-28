package settings

import (
	"fmt"
	"os"
)

// YOUTUBE_CACHE_EXPIRATION_SECONDS는 YouTubeConfig.CacheExpiration으로 읽혔지만 그 필드를 소비하는 코드가 없어
// 값이 어디에도 쓰이지 않았다(2026-09-26 stack audit A3). DEC-20260731-legacy-fade-out-no-dual-path에 따라
// 필드와 env 읽기를 지우고, 값을 읽지 않고 존재만으로 fail-closed 하는 종단 경로로만 남긴다.
// 도입 리비전: stack-audit T05(PLN-20260926-stack-audit-refactoring).
//
// 배포 선행 조건이자 제거 조건: settings.LoadConfig를 쓰는 runtime(hololive-api, alarm-worker)의 모든 배포 env
// 파일(중앙 compose.env와 그 stack-secrets 원본)에 YOUTUBE_CACHE_EXPIRATION_SECONDS 키가 0건임을 hololive-bot-ops로
// 확인한다. 이 가드가 든 release가 전 호스트에 배포된 뒤 이 파일, 테스트, loadYouTubeConfig의 호출을 함께 삭제한다.
// 재검토 기한: remove_after = "2026-12-31".
//
// YOUTUBE_VIDEO_RSS_BACKOFF_TTL_SECONDS는 GetRecentVideos의 RSS 폴백 뒤 HTML을 건너뛰는 backoff 상태의 TTL이었다.
// DEC-20260926-hololive-source-fallbacks-retirement로 RSS 폴백과 backoff 상태를 지워 값이 아무것도 선택하지 않으므로
// 같은 방식으로 존재만으로 거절한다. 도입 리비전: stack-audit T19(PLN-20260926-stack-audit-refactoring,
// holo-youtube-recent-videos-rss-fallback). 제거 조건은 위와 같다: 중앙 compose.env·bot.env·alarm-worker.env와 그
// stack-secrets master 사본, 실행 중 프로세스 env에 이 키가 0건임을 hololive-bot-ops로 확인하고 이 가드가 든 release가
// 배포된 뒤 이 항목과 테스트를 삭제한다. 재검토 기한: remove_after = "2026-12-31".
var retiredYouTubeConfigEnvKeys = []string{
	"YOUTUBE_CACHE_EXPIRATION_SECONDS",
	"YOUTUBE_VIDEO_RSS_BACKOFF_TTL_SECONDS",
}

func rejectRetiredYouTubeConfigEnv() error {
	for _, key := range retiredYouTubeConfigEnvKeys {
		if _, found := os.LookupEnv(key); found {
			return fmt.Errorf("%s is retired: the value had no consumer; remove it from the runtime env", key)
		}
	}

	return nil
}
