package server

import (
	"log/slog"

	sharedserver "github.com/kapu/hololive-shared/pkg/server"
)

// MemberNewsWeeklyScheduler: 구독 멤버 뉴스 주간 스케줄러 인터페이스
type MemberNewsWeeklyScheduler = sharedserver.MemberNewsWeeklyScheduler

// TriggerHandler: 내부 트리거 API 핸들러 (admin-api에서 스케줄러 수동 실행용)
type TriggerHandler = sharedserver.TriggerHandler

// NewTriggerHandler: TriggerHandler를 생성합니다.
func NewTriggerHandler(
	majorEvent MajorEventScheduler,
	majorEventMonthly MajorEventMonthlyScheduler,
	memberNewsWeekly MemberNewsWeeklyScheduler,
	logger *slog.Logger,
) *TriggerHandler {
	return sharedserver.NewTriggerHandler(majorEvent, majorEventMonthly, memberNewsWeekly, logger)
}
