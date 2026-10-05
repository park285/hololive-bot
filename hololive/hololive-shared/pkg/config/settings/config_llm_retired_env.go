package settings

import (
	"fmt"
	"os"
)

// LLM_MONTHLY_TOKEN_CEILING은 월 토큰 "상한" 경고와 Valkey 월 카운터(llm:cost:tokens:YYYY-MM)를 켜던 키다.
// DEC-20260926-hololive-llm-token-ceiling-retirement로 상한과 카운터를 지우고 토큰 메트릭은 항상 기록하므로,
// 키가 남아 있으면 운영자가 기대하는 상한이 없다는 사실을 숨기게 된다. 값을 읽지 않고 존재만으로 거절한다.
// 키 도입 커밋: 3978c301b. 가드 도입 리비전: stack-audit 2026-09-26(PLN-20260926-stack-audit-refactoring T09).
//
// 배포 선행 조건이자 제거 조건: hololive-api(bot·admin·llm plane)와 alarm-worker의 컨테이너 env 원천인 중앙
// /etc/stack-secrets/hololive-bot/bot.env(HOLOLIVE_API_ENV_FILE)와 alarm-worker.env(HOLOLIVE_ALARM_WORKER_ENV_FILE),
// 그 stack-secrets master 사본, 그리고 실행 중 프로세스 env에 LLM_MONTHLY_TOKEN_CEILING 키가 0건이어야 한다.
// 중앙 compose.env·AP ap-compose.env는 compose 보간 전용이고 prod compose의 environment 목록에 이 키가 없어
// 프로세스 env로 가지 않는다. T18(2026-09-26)에서 모든 hololive env에 0건임을 확인했다. 이 가드가 든 release가 중앙 호스트에
// 배포된 뒤 이 파일, 테스트, RejectRetiredRuntimeEnv·LoadLLMSchedulerRuntime의 호출을 함께 삭제한다.
// 재검토 기한: remove_after = "2026-12-31".
var retiredLLMEnvKeys = []string{
	"LLM_MONTHLY_TOKEN_CEILING",
}

// RejectRetiredLLMEnv는 퇴역한 LLM env 키가 프로세스 env에 있으면(빈 값 포함) 오류를 돌려준다.
func RejectRetiredLLMEnv() error {
	for _, key := range retiredLLMEnvKeys {
		if _, found := os.LookupEnv(key); found {
			return fmt.Errorf("%s is retired: the monthly token ceiling and its Valkey counter were removed; remove it from the runtime env", key)
		}
	}

	return nil
}
