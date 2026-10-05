package settings

import (
	"fmt"
	"os"
)

// SERVICES_LLM_SERVER_HEALTH_URL은 SERVICES_LLM_SCHEDULER_HEALTH_URL이 비었을 때 envutil.StringAny로 이어 읽던
// 이전 이름이다(alias 체인 도입 커밋: afb4fd49b). SERVICES_LLM_SCHEDULER_HEALTH_URL 하나를 정본으로 두고, 키가 남아
// 있으면 그 값이 쓰인다고 기대하게 되므로 값을 읽지 않고 존재만으로(빈 값 포함) 거절한다.
// 가드 도입 리비전: stack-audit 2026-09-26(PLN-20260926-stack-audit-refactoring T10).
//
// 배포 선행 조건이자 제거 조건: settings.RejectRetiredRuntimeEnv를 부르는 runtime(hololive-api bot·admin plane, alarm-worker)의
// 컨테이너 env 원천인 중앙 compose.env, bot.env(HOLOLIVE_API_ENV_FILE)와 alarm-worker.env(HOLOLIVE_ALARM_WORKER_ENV_FILE),
// 그 stack-secrets master 사본, 그리고 실행 중 프로세스 env에 SERVICES_LLM_SERVER_HEALTH_URL 키가 0건이어야 한다.
// T18(2026-09-26)은 이 키가 없고 hololive-api가 SERVICES_LLM_SCHEDULER_HEALTH_URL을 쓰는 것을 확인했다. 이 가드가 든
// release가 중앙 호스트에 배포된 뒤 이 파일, 테스트, RejectRetiredRuntimeEnv(config_build.go)의 호출을 함께 삭제한다.
// 재검토 기한: remove_after = "2026-12-31".
var retiredServicesEnvKeys = []string{
	"SERVICES_LLM_SERVER_HEALTH_URL",
}

func rejectRetiredServicesEnv() error {
	for _, key := range retiredServicesEnvKeys {
		if _, found := os.LookupEnv(key); found {
			return fmt.Errorf("%s is retired: use SERVICES_LLM_SCHEDULER_HEALTH_URL; remove it from the runtime env", key)
		}
	}

	return nil
}
