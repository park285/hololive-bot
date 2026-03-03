package membernews

import (
	"context"
	"log/slog"

	"github.com/kapu/hololive-llm-sched/internal/service/membernews/internal/model"
	mnscheduler "github.com/kapu/hololive-llm-sched/internal/service/membernews/scheduler"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/delivery"
)

type (
	Scheduler        = mnscheduler.Scheduler
	MonthlyScheduler = mnscheduler.MonthlyScheduler
)

const (
	WeeklyScheduleWeekday = mnscheduler.WeeklyScheduleWeekday
	WeeklyScheduleHourKST = mnscheduler.WeeklyScheduleHourKST

	MonthlyScheduleDay     = mnscheduler.MonthlyScheduleDay
	MonthlyScheduleHourKST = mnscheduler.MonthlyScheduleHourKST
)

// NOTE: 기존 API 호환을 위해 root 패키지에 unexported 인터페이스 이름을 유지합니다.
type digestService = model.DigestService

// outboxEnqueuer: outbox enqueue 연산 인터페이스 (테스트 mock 용도).
type outboxEnqueuer interface {
	Enqueue(ctx context.Context, kind domain.DeliveryOutboxKind, periodKey, roomID, message string) error
}

// NewScheduler: 주간 뉴스 자동 발송 스케줄러 생성. (호환 wrapper)
func NewScheduler(
	service digestService,
	formatter DigestFormatter,
	locker delivery.NotificationLocker,
	outboxRepo outboxEnqueuer,
	logger *slog.Logger,
) *Scheduler {
	return mnscheduler.NewScheduler(service, formatter, locker, outboxRepo, logger)
}

// NewMonthlyScheduler: 월간 뉴스 자동 발송 스케줄러 생성. (호환 wrapper)
func NewMonthlyScheduler(
	service digestService,
	formatter DigestFormatter,
	locker delivery.NotificationLocker,
	outboxRepo outboxEnqueuer,
	logger *slog.Logger,
) *MonthlyScheduler {
	return mnscheduler.NewMonthlyScheduler(service, formatter, locker, outboxRepo, logger)
}
