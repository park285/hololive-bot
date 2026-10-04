package settings

import (
	"fmt"
	"os"
)

// INSTANCE_ID는 분산 sliding window limiter의 member 구분자를 명시하던 키였다. 값이 없으면 hostname, random,
// "local" 순으로 내려갔다. T18(2026-09-26)에서 어느 운영 env에도 이 키가 없음을 확인해 hostname을 유일한 필수 출처로
// 정했다(DEC-20260926-hololive-legacy-env-config-retirement, holo-ratelimit-instance-id-fallback-chain). 키가 남아 있으면
// 그 값이 쓰인다고 오해하므로 값을 읽지 않고 존재만으로(빈 값 포함) 거절한다.
// 가드 도입 리비전: stack-audit 2026-09-26(PLN-20260926-stack-audit-refactoring T19).
//
// 거절은 limiter 생성 경로가 아니라 LoadConfig runtime의 rejectRetiredRuntimeEnv에서 한다. 분산 limiter를 끈 설정
// (HOLODEX_DISTRIBUTED_RATELIMIT_ENABLED=false)에서도 키가 남아 있으면 기동을 막기 위해서다.
//
// 배포 선행 조건이자 제거 조건: LoadConfig runtime(hololive-api bot·admin plane, alarm-worker)의 env 원천인 중앙
// compose.env·bot.env·alarm-worker.env, 그 stack-secrets master 사본, 그리고 실행 중 프로세스 env에 INSTANCE_ID가 0건임을
// hololive-bot-ops로 확인한다(T18 확인 완료). 이 가드가 든 release가 중앙 호스트에 배포된 뒤 이 파일, 테스트,
// rejectRetiredRuntimeEnv(config_build.go)의 호출을 함께 삭제한다. 재검토 기한: remove_after = "2026-12-31".
const retiredRateLimiterInstanceIDEnvKey = "INSTANCE_ID"

// rejectRetiredRateLimiterInstanceIDEnv는 퇴역한 INSTANCE_ID 키가 env에 있으면 오류를 돌려준다.
func rejectRetiredRateLimiterInstanceIDEnv() error {
	if _, found := os.LookupEnv(retiredRateLimiterInstanceIDEnvKey); found {
		return fmt.Errorf("%s is retired: the rate limiter uses the hostname as its only instance id; remove it from the runtime env", retiredRateLimiterInstanceIDEnvKey)
	}

	return nil
}
