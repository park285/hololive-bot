package settings

import (
	"fmt"
	"os"
)

// OFFICIAL_SCHEDULE_CACHE_EXPIRY_SECONDS는 OfficialScheduleConfig.CacheExpiry로 읽혀 htmlscraper가 channel schedule
// 보충 결과를 Valkey에 캐시하는 TTL로만 쓰였다. DEC-20260926-hololive-source-fallbacks-retirement로 그 보충 경로와
// 캐시를 지워 값이 아무것도 선택하지 않으므로, DEC-20260731-legacy-fade-out-no-dual-path에 따라 필드와 env 읽기를
// 지우고 값을 읽지 않고 존재만으로(빈 값 포함) 거절한다. 공식 일정 페이지 캐시는 OFFICIAL_SCHEDULE_PAGE_CACHE_TTL_SECONDS
// 하나가 소유한다. 도입 리비전: stack-audit T19(PLN-20260926-stack-audit-refactoring,
// holo-holodex-channel-schedule-scraper-fallback).
//
// 배포 선행 조건이자 제거 조건: settings.RejectRetiredRuntimeEnv를 부르는 runtime(hololive-api
// bot·admin plane, alarm-worker)의 env 원천인 중앙 compose.env, bot.env(HOLOLIVE_API_ENV_FILE)와
// alarm-worker.env(HOLOLIVE_ALARM_WORKER_ENV_FILE), 그 stack-secrets master 사본, 실행 중 프로세스 env에 이 키가
// 0건임을 hololive-bot-ops로 확인한다. 이 가드가 든 release가 중앙 호스트에 배포된 뒤 이 파일, 테스트,
// loadOfficialScheduleConfig의 호출을 함께 삭제한다. 재검토 기한: remove_after = "2026-12-31".
var retiredOfficialScheduleEnvKeys = []string{
	"OFFICIAL_SCHEDULE_CACHE_EXPIRY_SECONDS",
}

func rejectRetiredOfficialScheduleEnv() error {
	for _, key := range retiredOfficialScheduleEnvKeys {
		if _, found := os.LookupEnv(key); found {
			return fmt.Errorf("%s is retired: the value had no consumer; remove it from the runtime env", key)
		}
	}

	return nil
}
