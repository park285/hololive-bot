package config

import (
	"fmt"
	"os"
)

// 두 key는 alarm-worker의 Karing opt-in flag였다. DEC-20260926-hololive-karing-egress-disposition(DEC-20260904 대체)으로
// alarm-worker 알림은 Karing template을 쓰지 않으므로 어떤 값도 동작을 선택하지 않는다. 값을 읽지 않고 존재만으로
// (빈 값·작업 디렉터리 .env 포함) 거절한다. 가드 도입 리비전: b05282b2b. T17 형식 보강: stack-audit 2026-09-26.
//
// 배포 선행 조건이자 제거 조건: alarm-worker의 env 원천인 중앙 alarm-worker.env(HOLOLIVE_ALARM_WORKER_ENV_FILE)와
// compose.env, 그 stack-secrets master 사본, 실행 중 alarm-worker 프로세스 env에 두 key가 0건이어야 한다.
// T18(2026-09-26)에서 alarm-worker.env·compose.env에 0건임을 확인했다. 그 상태로 이 가드가 든 release가 중앙 호스트에
// 배포된 뒤 이 파일, 테스트, LoadRuntime의 호출, ci-notification-egress-gate.sh의 가드 파일 예외를 함께 삭제한다.
// 재검토 기한: remove_after = "2026-12-31".
var retiredNotificationEgressEnvKeys = []string{
	"YOUTUBE_OUTBOX_KARING_ENABLED",
	"ALARM_DISPATCH_KARING_ENABLED",
}

func rejectRetiredNotificationEgressEnv() error {
	for _, key := range retiredNotificationEgressEnvKeys {
		if _, found := os.LookupEnv(key); found {
			return fmt.Errorf("%s is retired: alarm-worker notifications never use Karing; remove it from the runtime env", key)
		}
	}

	return nil
}
