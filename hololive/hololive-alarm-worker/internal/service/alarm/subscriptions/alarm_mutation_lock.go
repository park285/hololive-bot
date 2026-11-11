package subscriptions

import "time"

// 반환한 시작 시각은 lock 대기 전이므로, 이 시각으로 잰 작업 시간 지표는 대기를 포함한 전체 응답 시간이다.
func (as *AlarmService) lockCacheMutation(operation string) time.Time {
	startedAt := time.Now()

	as.cacheMutationMu.Lock()
	observeAlarmMutationLockWait(operation, startedAt)

	return startedAt
}
