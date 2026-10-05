package settings

import (
	"fmt"
	"os"
)

// HOLODEX_LIVE_STATUS_FALLBACK_*는 Holodex /users/live 실패 때 채널별 YouTube scraper로 live-status를 채우던
// 2차 경로의 한도였다. DEC-20260926-hololive-live-status-scraper-fallback-removal로 그 경로를 지웠으므로 키가
// 남아 있으면 운영자가 기대하는 fallback이 없다는 사실을 숨긴다. 값을 읽지 않고 존재만으로 거절한다.
// 키 도입 커밋: c79c021dc. 가드 도입 리비전: stack-audit 2026-09-26(PLN-20260926-stack-audit-refactoring T09).
//
// 배포 선행 조건이자 제거 조건: settings.RejectRetiredRuntimeEnv를 부르는 runtime(hololive-api bot·admin plane, alarm-worker)의
// 컨테이너 env 원천인 중앙 /etc/stack-secrets/hololive-bot/bot.env(HOLOLIVE_API_ENV_FILE)와
// alarm-worker.env(HOLOLIVE_ALARM_WORKER_ENV_FILE), 그 stack-secrets master 사본, 그리고 실행 중 프로세스 env에
// 아래 키가 0건임을 hololive-bot-ops로 확인한다. 중앙 compose.env·AP ap-compose.env는 compose 보간 전용이고 prod
// compose의 environment 목록에 이 키가 없어 프로세스 env로 가지 않으며, AP overlay는 두 서비스를 central-only로 꺼 둔다.
// 이 가드가 든 release가 중앙 호스트에 배포된 뒤 이 파일, 테스트, RejectRetiredRuntimeEnv(config_build.go)의 호출을 함께 삭제한다.
// 재검토 기한: remove_after = "2026-12-31".
var retiredHolodexLiveStatusFallbackEnvKeys = []string{
	"HOLODEX_LIVE_STATUS_FALLBACK_MAX_PER_CYCLE",
	"HOLODEX_LIVE_STATUS_FALLBACK_WALL_CLOCK_BUDGET_SECONDS",
	"HOLODEX_LIVE_STATUS_FALLBACK_DEADLINE_MARGIN_MS",
}

func rejectRetiredHolodexLiveStatusFallbackEnv() error {
	for _, key := range retiredHolodexLiveStatusFallbackEnvKeys {
		if _, found := os.LookupEnv(key); found {
			return fmt.Errorf("%s is retired: the live-status scraper fallback was removed; remove it from the runtime env", key)
		}
	}

	return nil
}
