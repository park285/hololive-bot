package settings

import (
	"fmt"
	"os"
)

// ScraperConfig는 2026-08-25 퇴역한 youtube producer의 scheduler·poll·snapshot·channel health·backfill 설정과
// 스크레이퍼 SOCKS5 proxy 토글을 담았다. RejectRetiredRuntimeEnv runtime(hololive-api, alarm-worker)은 proxy 외에는 어느 필드도
// 소비하지 않았고, proxy는 운영 env에서 늘 꺼져 있었다(T18 2026-09-26: 모든 youtube-collector.env에 빈 값).
// DEC-20260926-hololive-legacy-env-config-retirement와 DEC-20260926-hololive-youtube-producer-budget-retired로 ScraperConfig,
// 그 loader·검증·*OrDefault, proxy client·런타임 토글·admin 설정 API의 scraperProxyEnabled를 지웠다. 키가 남아 있으면
// 운영자가 기대하는 설정이 적용된다고 오해하므로 값을 읽지 않고 존재만으로(빈 값 포함) 거절한다.
// 가드 도입 리비전: stack-audit 2026-09-26(PLN-20260926-stack-audit-refactoring T19, holo-scraper-config-producer-era-fields,
// holo-api-scraper-proxy-settings-http-field).
//
// 배포 선행 조건이자 제거 조건: settings.RejectRetiredRuntimeEnv를 부르는 runtime(hololive-api bot·admin plane, alarm-worker)의 env 원천인
// 중앙 compose.env·bot.env(HOLOLIVE_API_ENV_FILE)·alarm-worker.env(HOLOLIVE_ALARM_WORKER_ENV_FILE), 그 stack-secrets master
// 사본, 그리고 실행 중 프로세스 env에 아래 키가 0건임을 hololive-bot-ops로 확인한다. SCRAPER_PROXY_ENABLED·SCRAPER_PROXY_URL은
// youtube-collector도 같은 기준으로 거절하므로(collector/retired_env.go) 모든 youtube-collector.env와 master 사본에서도 먼저
// 지운다(T18은 이 두 키가 모든 youtube-collector.env에 남아 있음을 확인했다). 이 가드가 든 release가 중앙 호스트에 배포된 뒤
// 이 파일, 테스트, RejectRetiredRuntimeEnv(config_build.go)의 호출을 함께 삭제한다. 재검토 기한: remove_after = "2026-12-31".
var retiredScraperConfigEnvKeys = []string{
	"SCRAPER_PROXY_ENABLED",
	"SCRAPER_PROXY_URL",
	"SCRAPER_SCHEDULER_WORKER_COUNT",
	"SCRAPER_SCHEDULER_POLL_TIMEOUT_SECONDS",
	"SCRAPER_SCHEDULER_ERROR_BACKOFF_MIN_SECONDS",
	"SCRAPER_SCHEDULER_ERROR_BACKOFF_MAX_SECONDS",
	"SCRAPER_POLL_VIDEOS_INTERVAL_SECONDS",
	"SCRAPER_POLL_SHORTS_INTERVAL_SECONDS",
	"SCRAPER_POLL_COMMUNITY_INTERVAL_SECONDS",
	"SCRAPER_POLL_STATS_INTERVAL_SECONDS",
	"SCRAPER_POLL_LIVE_INTERVAL_SECONDS",
	"SCRAPER_POLL_TIERING_ENABLED",
	"SCRAPER_SNAPSHOT_ENABLED",
	"SCRAPER_SNAPSHOT_DIR",
	"SCRAPER_SNAPSHOT_MAX_BODY_BYTES",
	"SCRAPER_SNAPSHOT_MIN_INTERVAL_SECONDS",
	"SCRAPER_CHANNEL_HEALTH_ENABLED",
	"SCRAPER_CHANNEL_HEALTH_ENFORCE",
	"SCRAPER_CHANNEL_HEALTH_TTL_SECONDS",
	"SCRAPER_CHANNEL_HEALTH_PARSER_DRIFT_BASE_SECONDS",
	"SCRAPER_CHANNEL_HEALTH_PARSER_DRIFT_MAX_SECONDS",
	"SCRAPER_CHANNEL_HEALTH_TRANSPORT_BASE_SECONDS",
	"SCRAPER_CHANNEL_HEALTH_TRANSPORT_MAX_SECONDS",
	"SCRAPER_CHANNEL_HEALTH_TIMEOUT_BASE_SECONDS",
	"SCRAPER_CHANNEL_HEALTH_TIMEOUT_MAX_SECONDS",
	"SCRAPER_CHANNEL_HEALTH_HTTP_STATUS_BASE_SECONDS",
	"SCRAPER_CHANNEL_HEALTH_HTTP_STATUS_MAX_SECONDS",
	"SCRAPER_CHANNEL_HEALTH_SUCCESS_DECAY_STEPS",
	"SCRAPER_BACKFILL_ENABLED",
	"SCRAPER_BACKFILL_SHORTS_ENABLED",
	"SCRAPER_BACKFILL_SHORTS_INTERVAL_SECONDS",
	"SCRAPER_BACKFILL_LIVE_ENABLED",
	"SCRAPER_BACKFILL_LIVE_INTERVAL_SECONDS",
	"SCRAPER_BACKFILL_TARGET_GROUP",
}

// 아래 키는 이전 변경에서 읽기를 지운 뒤 가드 없이 무시해 왔다(SCRAPER_*_SECONDS·SCRAPER_WORKER_COUNT는 2026-08-02 scraper env
// 단일화, IRIS_SHARED_TOKEN은 webhook token 폴백 제거). 무시 동작을 고정하던 테스트를 거절 테스트로 바꾸고 같은 존재 기준으로
// 막는다(holo-removed-env-silently-ignored). T18(2026-09-26)은 SCRAPER_* 키가 runtime env에 없고, IRIS_SHARED_TOKEN은 runtime이
// 읽지 않는 중앙의 구 /etc/stack-secrets/hololive-bot/env에만 있음을 확인했다. 제거 조건과 재검토 기한은 위와 같다.
var retiredIgnoredLegacyEnvKeys = []string{
	"SCRAPER_VIDEOS_SECONDS",
	"SCRAPER_SHORTS_SECONDS",
	"SCRAPER_COMMUNITY_SECONDS",
	"SCRAPER_STATS_SECONDS",
	"SCRAPER_LIVE_SECONDS",
	"SCRAPER_WORKER_COUNT",
	"IRIS_SHARED_TOKEN",
}

func rejectRetiredScraperConfigEnv() error {
	for _, key := range retiredScraperConfigEnvKeys {
		if _, found := os.LookupEnv(key); found {
			return fmt.Errorf("%s is retired: the producer-era scraper config and the scraper proxy were removed; remove it from the runtime env", key)
		}
	}

	for _, key := range retiredIgnoredLegacyEnvKeys {
		if _, found := os.LookupEnv(key); found {
			return fmt.Errorf("%s is retired: it has no reader and was silently ignored; remove it from the runtime env", key)
		}
	}

	return nil
}
