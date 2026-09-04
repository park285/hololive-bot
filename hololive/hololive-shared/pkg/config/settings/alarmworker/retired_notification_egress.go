package alarmworker

import (
	"fmt"
	"os"
)

// DEC-20260904-hololive-karing-regular-chat-egress에 따라 이 key는 더 이상 동작을 선택하지
// 않는다. 모든 alarm-worker runtime file에서 key가 사라진 상태로 한 release가 배포되면 가드 제거를 검토한다.
var retiredNotificationEgressEnvKeys = []string{
	"YOUTUBE_OUTBOX_KARING_ENABLED",
	"ALARM_DISPATCH_KARING_ENABLED",
}

func rejectRetiredNotificationEgressEnv() error {
	for _, key := range retiredNotificationEgressEnvKeys {
		if _, found := os.LookupEnv(key); found {
			return fmt.Errorf("%s is retired; Karing routing is selected by confirmed room type", key)
		}
	}

	return nil
}
