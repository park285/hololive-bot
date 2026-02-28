package app

import (
	"log/slog"

	sharedserver "github.com/kapu/hololive-shared/pkg/server"
)

// ProvideTriggerHandler: 내부 트리거 핸들러를 생성하여 제공합니다.
func ProvideTriggerHandler(
	majorEventScheduler sharedserver.MajorEventScheduler,
	majorEventMonthlyScheduler sharedserver.MajorEventMonthlyScheduler,
	memberNewsWeeklyScheduler sharedserver.MemberNewsWeeklyScheduler,
	logger *slog.Logger,
) *sharedserver.TriggerHandler {
	return sharedserver.NewTriggerHandler(majorEventScheduler, majorEventMonthlyScheduler, memberNewsWeeklyScheduler, logger)
}
