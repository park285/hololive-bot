// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/kapu/hololive-llm-sched/internal/schedulerkit"
	"github.com/kapu/hololive-llm-sched/internal/service/membernews/internal/model"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/delivery"
)

const maxConcurrentDigests = 5

const (
	// WeeklyScheduleWeekday: 주간 발송 요일 (runtime 로그에서도 참조)
	WeeklyScheduleWeekday = time.Monday

	// WeeklyScheduleHourKST: 주간 발송 시각 (KST, runtime 로그에서도 참조)
	WeeklyScheduleHourKST   = 9
	weeklyScheduleMinuteKST = 0
)

// outboxEnqueuer: outbox enqueue 연산 인터페이스 (테스트 mock 용도)
type outboxEnqueuer interface {
	Enqueue(ctx context.Context, kind domain.DeliveryOutboxKind, periodKey, roomID, message string) error
}

type Scheduler struct {
	service    model.DigestService
	formatter  model.DigestFormatter
	locker     delivery.NotificationLocker
	outboxRepository outboxEnqueuer
	logger     *slog.Logger
	runtime    *schedulerkit.Runtime
}

func NewScheduler(
	service model.DigestService,
	formatter model.DigestFormatter,
	locker delivery.NotificationLocker,
	outboxRepository outboxEnqueuer,
	logger *slog.Logger,
) *Scheduler {
	if logger == nil {
		logger = slog.Default()
	}
	if locker == nil {
		locker = delivery.NewLocker(nil, logger)
	}

	return &Scheduler{
		service:    service,
		formatter:  formatter,
		locker:     locker,
		outboxRepository: outboxRepository,
		logger:     logger,
		runtime:    schedulerkit.NewRuntime(),
	}
}

func (s *Scheduler) SetClock(clockFn func() time.Time) {
	if s == nil {
		return
	}
	s.runtime.SetClock(clockFn)
}

func (s *Scheduler) clock() time.Time {
	if s == nil || s.runtime == nil {
		return time.Now()
	}
	return s.runtime.Now()
}

func (s *Scheduler) Start(ctx context.Context) {
	if s == nil {
		return
	}
	s.runtime.Start(ctx, schedulerkit.Config{
		Logger:           s.logger,
		WaitingLog:       "Member news scheduler waiting",
		ContextStopLog:   "Member news scheduler stopped by context",
		StopLog:          "Member news scheduler stopped",
		CalculateNextRun: s.calculateNextRun,
		OnTick: func(ctx context.Context) {
			if err := s.SendWeeklyDigest(ctx); err != nil {
				s.logger.Error("Member news weekly send failed", slog.String("error", err.Error()))
			}
		},
	})
}

func (s *Scheduler) Stop() {
	if s == nil {
		return
	}
	s.runtime.Stop()
}

func (s *Scheduler) calculateNextRun(now time.Time) time.Time {
	nowKST := now.In(model.KST)

	daysUntilMonday := (int(time.Monday) - int(nowKST.Weekday()) + 7) % 7
	target := time.Date(
		nowKST.Year(), nowKST.Month(), nowKST.Day()+daysUntilMonday,
		WeeklyScheduleHourKST, weeklyScheduleMinuteKST, 0, 0, model.KST,
	)

	if !target.After(nowKST) {
		target = target.AddDate(0, 0, 7)
	}
	return target
}

func (s *Scheduler) SendWeeklyDigest(ctx context.Context) error {
	if s == nil {
		return fmt.Errorf("member news scheduler is nil")
	}
	if s.service == nil {
		return fmt.Errorf("member news service is nil")
	}

	weekKey := startOfWeek(s.clock()).Format("2006-01-02")
	lockKey := fmt.Sprintf("membernews:lock:weekly:%s", weekKey)
	return runDigestDispatch(ctx, s.service, s.locker, s.logger, digestDispatchConfig{
		lockKey:           lockKey,
		periodKey:         weekKey,
		periodFieldName:   "week_key",
		lockHeldMessage:   "Member news weekly execution skipped: lock already acquired",
		emptyRoomsMessage: "Member news weekly skipped: no subscribed room",
		resultMessage:     "Member news weekly result",
		allFailedMessage:  "all %d room(s) failed to receive member news",
		processRoom:       s.processRoomDigest,
	})
}

// processRoomDigest: 단일 room의 주간 다이제스트 생성 + outbox enqueue.
func (s *Scheduler) processRoomDigest(ctx context.Context, weekKey, roomID string) delivery.SendResult {
	return processDigestForRoom(ctx, s.service, s.formatter, s.outboxRepository, s.logger,
		model.PeriodWeekly, domain.DeliveryKindMemberNewsWeekly, weekKey, roomID, "🗞️ 이번주 구독 멤버 뉴스")
}

func startOfWeek(t time.Time) time.Time {
	kstNow := t.In(model.KST)
	daysFromMonday := (int(kstNow.Weekday()) - int(time.Monday) + 7) % 7
	start := kstNow.AddDate(0, 0, -daysFromMonday)
	return time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, model.KST)
}
