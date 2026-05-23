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

const (
	// MonthlyScheduleHourKST: 월간 발송 시각 (KST, runtime 로그에서도 참조)
	MonthlyScheduleHourKST   = 10
	monthlyScheduleMinuteKST = 0

	// MonthlyScheduleDay: 월간 발송 일자 (runtime 로그에서도 참조)
	MonthlyScheduleDay = 1
)

type MonthlyScheduler struct {
	service          model.DigestService
	formatter        model.DigestFormatter
	locker           delivery.NotificationLocker
	outboxRepository outboxEnqueuer
	logger           *slog.Logger
	runtime          *schedulerkit.Runtime
}

func NewMonthlyScheduler(
	service model.DigestService,
	formatter model.DigestFormatter,
	locker delivery.NotificationLocker,
	outboxRepository outboxEnqueuer,
	logger *slog.Logger,
) *MonthlyScheduler {
	if logger == nil {
		logger = slog.Default()
	}
	if locker == nil {
		locker = delivery.NewLocker(nil, logger)
	}

	return &MonthlyScheduler{
		service:          service,
		formatter:        formatter,
		locker:           locker,
		outboxRepository: outboxRepository,
		logger:           logger,
		runtime:          schedulerkit.NewRuntime(),
	}
}

func (s *MonthlyScheduler) SetClock(clockFn func() time.Time) {
	if s == nil {
		return
	}
	s.runtime.SetClock(clockFn)
}

func (s *MonthlyScheduler) clock() time.Time {
	if s == nil || s.runtime == nil {
		return time.Now()
	}
	return s.runtime.Now()
}

func (s *MonthlyScheduler) Start(ctx context.Context) {
	if s == nil {
		return
	}
	s.runtime.Start(ctx, schedulerkit.Config{
		Logger:           s.logger,
		WaitingLog:       "Member news monthly scheduler waiting",
		ContextStopLog:   "Member news monthly scheduler stopped by context",
		StopLog:          "Member news monthly scheduler stopped",
		CalculateNextRun: s.calculateNextRun,
		OnTick: func(ctx context.Context) {
			if err := s.SendMonthlyDigest(ctx); err != nil {
				s.logger.Error("Member news monthly send failed", slog.String("error", err.Error()))
			}
		},
	})
}

func (s *MonthlyScheduler) Stop() {
	if s == nil {
		return
	}
	s.runtime.Stop()
}

func (s *MonthlyScheduler) calculateNextRun(now time.Time) time.Time {
	nowKST := now.In(model.KST)

	target := time.Date(
		nowKST.Year(), nowKST.Month(), MonthlyScheduleDay,
		MonthlyScheduleHourKST, monthlyScheduleMinuteKST, 0, 0, model.KST,
	)

	// 이미 지났으면 다음 달로
	if !target.After(nowKST) {
		target = target.AddDate(0, 1, 0)
	}
	return target
}

func (s *MonthlyScheduler) SendMonthlyDigest(ctx context.Context) error {
	if s == nil {
		return fmt.Errorf("member news monthly scheduler is nil")
	}
	if s.service == nil {
		return fmt.Errorf("member news service is nil")
	}

	monthKey := s.getMonthKey()
	lockKey := fmt.Sprintf("membernews:lock:monthly:%s", monthKey)
	return runDigestDispatch(ctx, s.service, s.locker, s.logger, digestDispatchConfig{
		lockKey:           lockKey,
		periodKey:         monthKey,
		periodFieldName:   "month_key",
		lockHeldMessage:   "Member news monthly execution skipped: lock already acquired",
		emptyRoomsMessage: "Member news monthly skipped: no subscribed room",
		resultMessage:     "Member news monthly result",
		allFailedMessage:  "all %d room(s) failed to receive monthly member news",
		processRoom:       s.processRoomDigest,
	})
}

// processRoomDigest: 단일 room의 월간 다이제스트 생성 + outbox enqueue.
func (s *MonthlyScheduler) processRoomDigest(ctx context.Context, monthKey, roomID string) delivery.SendResult {
	return processDigestForRoom(ctx, s.service, s.formatter, s.outboxRepository, s.logger,
		model.PeriodMonthly, domain.DeliveryKindMemberNewsMonthly, monthKey, roomID, "📅 이번달 구독 멤버 뉴스")
}

func (s *MonthlyScheduler) getMonthKey() string {
	now := s.clock().In(model.KST)
	return fmt.Sprintf("%d-%02d", now.Year(), now.Month())
}
