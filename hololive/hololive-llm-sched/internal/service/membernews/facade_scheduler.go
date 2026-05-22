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

// outboxEnqueuer: outbox enqueue 연산 인터페이스 (테스트 mock 용도).
type outboxEnqueuer interface {
	Enqueue(ctx context.Context, kind domain.DeliveryOutboxKind, periodKey, roomID, message string) error
}

func NewScheduler(
	service model.DigestService,
	formatter DigestFormatter,
	locker delivery.NotificationLocker,
	outboxRepository outboxEnqueuer,
	logger *slog.Logger,
) *Scheduler {
	return mnscheduler.NewScheduler(service, formatter, locker, outboxRepository, logger)
}

func NewMonthlyScheduler(
	service model.DigestService,
	formatter DigestFormatter,
	locker delivery.NotificationLocker,
	outboxRepository outboxEnqueuer,
	logger *slog.Logger,
) *MonthlyScheduler {
	return mnscheduler.NewMonthlyScheduler(service, formatter, locker, outboxRepository, logger)
}
