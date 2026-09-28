package settings

import (
	"fmt"
	"os"
)

// YOUTUBE_PRODUCER_* env는 2026-08-25 producer 퇴역(DEC-20260814-hololive-youtube-three-provider-convergence-v2)으로
// 소비자가 없어졌다. DEC-20260731-legacy-fade-out-no-dual-path에 따라 값을 읽지 않고 존재만으로 fail-closed 하는
// 종단 경로로만 남긴다. 도입 리비전은 9607db710이다.
//
// 이 가드는 loadYouTubeConfig를 거치는 runtime(hololive-api bot·admin plane, alarm-worker의 settings.LoadConfig)에서만
// 실행된다. 자체 RuntimeConfig를 쓰는 youtube-collector는 이 가드를 부르지 않는다(stack-audit 2026-09-26 재조사 정정).
// 제거 조건: 가드를 실행하는 runtime의 env 원천인 중앙 compose.env·bot.env(HOLOLIVE_API_ENV_FILE)·alarm-worker.env
// (HOLOLIVE_ALARM_WORKER_ENV_FILE)와 그 stack-secrets master 사본, 실행 중 프로세스 env에 YOUTUBE_PRODUCER_* 키가 0건이고,
// 그 상태로 이 가드가 든 release가 중앙 호스트에 배포된 뒤. T18(2026-09-26)에서 collector env를 포함한 모든 hololive env에
// 0건임을 확인했다.
// 삭제 리비전: 제거 조건을 충족한 뒤의 첫 hololive-bot release. 그 리비전에서 이 파일,
// config_youtube_producer_retired_test.go의 퇴역 가드 테스트 두 개, config.go의 rejectRetiredYouTubeProducerEnv 호출을
// 함께 삭제한다.
// 재검토 기한: remove_after = "2026-12-31".
var retiredYouTubeProducerEnvKeys = []string{
	"YOUTUBE_PRODUCER_FETCHER_ENGINE",
	"YOUTUBE_PRODUCER_REQUEST_INTERVAL_SECONDS",
	"YOUTUBE_PRODUCER_DISTRIBUTED_RATELIMIT_ENABLED",
	"YOUTUBE_PRODUCER_DISTRIBUTED_RATELIMIT_LIMIT",
	"YOUTUBE_PRODUCER_DISTRIBUTED_RATELIMIT_KEY_PREFIX",
	"YOUTUBE_PRODUCER_DISTRIBUTED_RATELIMIT_BUCKET_BASE",
	"YOUTUBE_PRODUCER_ACTIVE_ACTIVE_ENABLED",
	"YOUTUBE_PRODUCER_INSTANCE_ID",
	"YOUTUBE_PRODUCER_LEASE_NAMESPACE",
	"YOUTUBE_PRODUCER_GLOBAL_BUDGET_ENABLED",
	"YOUTUBE_PRODUCER_ACTIVE_ACTIVE_INSTANCE_COUNT",
	"YOUTUBE_PRODUCER_BUDGET_ACQUIRE_TIMEOUT_MS",
	"YOUTUBE_PRODUCER_BUDGET_YOUTUBE_SCRAPER_MAX_INFLIGHT",
	"YOUTUBE_PRODUCER_BUDGET_HOLODEX_LIVE_MAX_INFLIGHT",
	"YOUTUBE_PRODUCER_BUDGET_BROWSER_SNAPSHOT_MAX_INFLIGHT",
	"YOUTUBE_PRODUCER_BUDGET_BACKFILL_MAX_INFLIGHT",
	"YOUTUBE_PRODUCER_BUDGET_FALLBACK_MAX_INFLIGHT",
	"YOUTUBE_PRODUCER_BUDGET_CLEANUP_LIMIT",
	"YOUTUBE_PRODUCER_BUDGET_WINDOW_CHECK_ENABLED",
}

func rejectRetiredYouTubeProducerEnv() error {
	for _, key := range retiredYouTubeProducerEnvKeys {
		if _, found := os.LookupEnv(key); found {
			return fmt.Errorf("%s is retired; use YOUTUBE_REQUEST_INTERVAL_SECONDS and YOUTUBE_DISTRIBUTED_RATELIMIT_*", key)
		}
	}

	return nil
}
