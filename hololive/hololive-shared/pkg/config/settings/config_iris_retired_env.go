package settings

import (
	"fmt"
	"os"
)

// IRIS_BASE_URL_FILE_SKIP_STAT_CHECKS=true는 live-compat overlay가 주입하던 호환 플래그로, production에서도
// IRIS_BASE_URL_FILE의 경로·소유·권한 stat 검사를 건너뛰게 했다. T18(2026-09-26)에서 중앙 runtime-config/iris_base_url이
// root 소유 0644로 검사를 통과함을 확인해 overlay 줄과 해석을 지웠다(stack-audit 2026-09-26 T11
// holo-iris-base-url-skip-stat-compat). 키가 남아 있으면 검사가 꺼져 있다고 오해하게 하므로 값을 읽지 않고 존재만으로
// 거절한다. 가드 도입 리비전: stack-audit 2026-09-26(PLN-20260926-stack-audit-refactoring T11).
//
// 배포 선행 조건이자 제거 조건: overlay 줄을 지운 이 release와 함께 배포한다. 대상은 settings.LoadConfig를 쓰는 runtime
// (hololive-api bot·admin plane, alarm-worker)의 env 원천인 중앙 bot.env·alarm-worker.env·compose.env, 그 stack-secrets
// master 사본, 그리고 실행 중 프로세스 env에 키가 0건임을 hololive-bot-ops로 확인한다. 이 가드가 든 release가 중앙
// 호스트에 배포된 뒤 이 파일, 테스트, rejectRetiredRuntimeEnv(config_build.go)의 호출을 함께 삭제한다. 재검토 기한: remove_after = "2026-12-31".
var retiredIrisEnvKeys = []string{
	"IRIS_BASE_URL_FILE_SKIP_STAT_CHECKS",
}

func rejectRetiredIrisEnv() error {
	for _, key := range retiredIrisEnvKeys {
		if _, found := os.LookupEnv(key); found {
			return fmt.Errorf("%s is retired: production always validates IRIS_BASE_URL_FILE stat; remove it from the runtime env", key)
		}
	}

	return nil
}
