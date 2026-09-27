package collector

import (
	"fmt"
	"os"
)

// SCRAPER_PROXY_ENABLED·SCRAPER_PROXY_URL은 youtubejs helper가 HTTP(S) proxy를 거쳐 YouTube에 접속하게 하던 설정이다.
// 운영 env에서는 늘 꺼져 있었고(T18 2026-09-26: 모든 youtube-collector.env에 빈 값), DEC-20260926-hololive-legacy-env-config-retirement의
// 퇴역한 scraper proxy와 함께 collector 설정의 proxy와 helper 기동 인자 전달을 지웠고, 뒤이어 helper bootstrap/health의 proxy
// 프로토콜과 Node ProxyAgent 경로·undici 의존성도 matched pair로 지워 helper에는 proxy 설정 경로가 없다. YOUTUBE_COLLECTOR_MAX_AGGREGATE_BYTES와
// YOUTUBE_COLLECTOR_YOUTUBEJS_TIMEOUT_SECONDS는 HC-013 이전 alias로, 읽기를 지운 뒤 가드 없이 무시해 왔다
// (holo-removed-env-silently-ignored). 키가 남아 있으면 그 값이 적용된다고 오해하므로 값을 읽지 않고 존재만으로(빈 값 포함) 거절한다.
// 가드 도입 리비전: stack-audit 2026-09-26(PLN-20260926-stack-audit-refactoring T19).
//
// 배포 선행 조건이자 제거 조건: youtube-collector의 env 원천인 중앙 youtube-collector.env(HOLOLIVE_YOUTUBE_COLLECTOR_ENV_FILE),
// compose AP(seoul)의 youtube-collector.env, host-native AP(osaka1·osaka2)의 collector env, 그 stack-secrets master 사본, 그리고
// 실행 중 프로세스 env에 아래 키가 0건임을 hololive-bot-ops로 확인한다. T18은 SCRAPER_PROXY_ENABLED·SCRAPER_PROXY_URL이 모든
// youtube-collector.env에 남아 있음을 확인했으므로, 이 가드가 든 release는 그 키를 지운 뒤에만 배포한다. 이 release가 중앙과 모든
// AP에 배포된 뒤 이 파일, 테스트, buildRuntimeConfig의 호출을 함께 삭제한다. 재검토 기한: remove_after = "2026-12-31".
var retiredCollectorEnvKeys = []string{
	"SCRAPER_PROXY_ENABLED",
	"SCRAPER_PROXY_URL",
	"YOUTUBE_COLLECTOR_MAX_AGGREGATE_BYTES",
	"YOUTUBE_COLLECTOR_YOUTUBEJS_TIMEOUT_SECONDS",
}

func rejectRetiredCollectorEnv() error {
	for _, key := range retiredCollectorEnvKeys {
		if _, found := os.LookupEnv(key); found {
			return fmt.Errorf("%s is retired: the collector no longer reads it; remove it from the youtube-collector env", key)
		}
	}

	return nil
}
