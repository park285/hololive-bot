package subscriptions

import "time"

func (as *AlarmService) lockCacheMutation(operation string) time.Time {
	startedAt := time.Now()

	as.cacheMutationMu.Lock()
	observeAlarmMutationLockWait(operation, startedAt)

	return startedAt
}
