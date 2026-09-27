package settings

import (
	"fmt"
	"os"
)

// SCRAPER_FETCHER_ENGINE은 net/http 하나만 남은 단일값 선택자였고, SCRAPER_BROWSER_DIAGNOSTIC_*는 production 호출자가
// 없는 browser snapshot 진단·blocked body 대체 fetch 경로의 설정이었다. 감사 T11 C2(stack-audit 2026-09-26)에서 그 경로와
// 필드를 지웠으므로, 키가 남아 있으면 운영자가 기대하는 선택이나 진단이 없다는 사실을 숨긴다. 값을 읽지 않고 존재만으로
// 거절한다. 이 가드는 SCRAPER_FETCHER_ENGINE의 값 가드(도입 774f86536, goscrapy 거절)를 대체한다.
// 가드 도입 리비전: stack-audit 2026-09-26(PLN-20260926-stack-audit-refactoring T11, holo-scraper-fetcher-engine-single-value).
//
// 배포 선행 조건이자 제거 조건: settings.LoadConfig를 쓰는 runtime(hololive-api bot·admin plane, alarm-worker)의 컨테이너
// env 원천인 중앙 /etc/stack-secrets/hololive-bot/bot.env(HOLOLIVE_API_ENV_FILE)와 alarm-worker.env
// (HOLOLIVE_ALARM_WORKER_ENV_FILE), 중앙 compose.env, 그 stack-secrets master 사본, 그리고 실행 중 프로세스 env에 아래 키가
// 0건임을 hololive-bot-ops로 확인한다. 이 키를 youtube-collector는 읽지 않는다. 이 가드가 든 release가 중앙 호스트에
// 배포된 뒤 이 파일, 테스트, rejectRetiredRuntimeEnv(config_build.go)의 호출을 함께 삭제한다. 재검토 기한: remove_after = "2026-12-31".
var retiredScraperFetchEnvKeys = []string{
	"SCRAPER_FETCHER_ENGINE",
	"SCRAPER_BROWSER_DIAGNOSTIC_ENABLED",
	"SCRAPER_BROWSER_DIAGNOSTIC_ENDPOINT",
	"SCRAPER_BROWSER_DIAGNOSTIC_TIMEOUT_SECONDS",
}

func rejectRetiredScraperFetchEnv() error {
	for _, key := range retiredScraperFetchEnvKeys {
		if _, found := os.LookupEnv(key); found {
			return fmt.Errorf("%s is retired: the scraper uses net/http only and the browser snapshot path was removed; remove it from the runtime env", key)
		}
	}

	return nil
}
