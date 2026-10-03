package envload

import (
	"fmt"
	"os"
)

// HOLODEX_API_KEY_1은 HOLODEX_API_KEY가 비었을 때 envutil.StringAny로 이어 읽던 이전 이름이다. 이 alias 체인은
// key pool(HOLODEX_API_KEY_1..N)을 없애면서 남긴 것이며 도입 커밋은 9441ca721이다.
// DEC-20260926-hololive-legacy-env-config-retirement로 HOLODEX_API_KEY 하나를 정본으로 정했다. 키가 남아 있으면
// 그 값이 쓰인다고 기대하게 되므로 값을 읽지 않고 존재만으로(빈 값 포함) 거절한다.
// 가드 도입 리비전: stack-audit 2026-09-26(PLN-20260926-stack-audit-refactoring T10).
//
// 배포 선행 조건이자 제거 조건: HOLODEX_API_KEY를 읽는 runtime(hololive-api, alarm-worker, youtube-collector)의 env
// 원천인 중앙 compose.env, bot.env(HOLOLIVE_API_ENV_FILE), alarm-worker.env(HOLOLIVE_ALARM_WORKER_ENV_FILE),
// youtube-collector.env(HOLOLIVE_YOUTUBE_COLLECTOR_ENV_FILE), AP의 ap-compose.env·youtube-collector.env와 host-native
// collector env, 그 stack-secrets master 사본, 그리고 실행 중 프로세스 env에 HOLODEX_API_KEY_1 키가 0건이어야 한다.
// T18(2026-09-26)은 모든 hololive env에 빈 값으로 남아 있음을 확인했으므로, 이 가드가 든 release는 hololive-bot-ops가
// 그 키를 지운 뒤에만 배포한다. 이 release가 중앙과 모든 AP에 배포된 뒤 이 파일, 테스트, HolodexAPIKey의 호출을 함께 삭제한다.
// 재검토 기한: remove_after = "2026-12-31".
var retiredHolodexAPIKeyAliasEnvKeys = []string{
	"HOLODEX_API_KEY_1",
}

func rejectRetiredHolodexAPIKeyAliasEnv() error {
	for _, key := range retiredHolodexAPIKeyAliasEnvKeys {
		if _, found := os.LookupEnv(key); found {
			return fmt.Errorf("%s is retired: HOLODEX_API_KEY is the only Holodex key; remove it from the runtime env", key)
		}
	}

	return nil
}
