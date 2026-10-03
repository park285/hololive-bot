package settings

import (
	"fmt"
	"os"
)

// DELIVERY_OUTBOX_V3_HANDOFF_MODE(hololive-api llm plane)와 YOUTUBE_OUTBOX_V3_HANDOFF_MODE(alarm-worker)는 v2 digest와
// v1 YouTube 알림을 v3 dispatch ledger로 넘기던 off/shadow/cutover 선택자였다. DEC-20260926-hololive-outbox-v3-convergence로
// v1·v2 direct egress를 정본으로 두고 handoff 코드를 삭제했으므로, 키가 남아 있으면 운영자가 기대하는 shadow·cutover가
// 없다는 사실을 숨긴다. 값을 읽지 않고 존재만으로(빈 값 포함) 거절한다.
// 키 도입 커밋: 333665c1a. 가드 도입 리비전: stack-audit 2026-09-26(PLN-20260926-stack-audit-refactoring T19).
//
// 배포 선행 조건이자 제거 조건: 운영 값은 prod compose의 기본값(`${...:-off}`)으로만 들어갔고 T18(2026-09-26)에서 env 파일에
// 두 키가 0건임을 확인했다. 같은 변경에서 docker-compose.prod.yml의 두 environment 줄을 지웠으므로, 중앙
// /etc/stack-secrets/hololive-bot/bot.env(HOLOLIVE_API_ENV_FILE)·alarm-worker.env(HOLOLIVE_ALARM_WORKER_ENV_FILE)·compose.env,
// 그 stack-secrets master 사본, 실행 중 hololive-api·alarm-worker 프로세스 env에 두 키가 0건임을 hololive-bot-ops로 다시 확인한 뒤
// 이 가드가 든 release를 배포한다. 배포 뒤 이 파일, 테스트, rejectRetiredRuntimeEnv(config_build.go)와
// API LoadLLMSchedulerRuntime(internal/config/llm_scheduler.go)의 호출을 함께 삭제한다. 재검토 기한: remove_after = "2026-12-31".
var retiredOutboxV3HandoffEnvKeys = []string{
	"DELIVERY_OUTBOX_V3_HANDOFF_MODE",
	"YOUTUBE_OUTBOX_V3_HANDOFF_MODE",
}

// RejectRetiredOutboxV3HandoffEnv는 퇴역한 outbox v3 handoff mode 키가 프로세스 env에 있으면(빈 값 포함) 오류를 돌려준다.
func RejectRetiredOutboxV3HandoffEnv() error {
	for _, key := range retiredOutboxV3HandoffEnvKeys {
		if _, found := os.LookupEnv(key); found {
			return fmt.Errorf("%s is retired: the outbox v3 handoff was removed and v1/v2 direct egress is canonical; remove it from the runtime env", key)
		}
	}

	return nil
}
