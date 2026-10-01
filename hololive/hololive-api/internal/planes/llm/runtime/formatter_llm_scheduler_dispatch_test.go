package runtime

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/park285/shared-go/v2/pkg/outputguard"
	"github.com/park285/shared-go/v2/pkg/promptguard"
	"github.com/stretchr/testify/require"

	eventscheduler "github.com/kapu/hololive-api/internal/planes/llm/internal/service/majorevent/scheduler"
	"github.com/kapu/hololive-shared/pkg/domain"
)

type renderFailureEvents struct{ marked bool }

func (*renderFailureEvents) GetSubscribedRooms(context.Context) ([]*domain.EventRoomSubscription, error) {
	return []*domain.EventRoomSubscription{{RoomID: "room"}}, nil
}

func (*renderFailureEvents) GetEventsByDateRange(context.Context, time.Time, time.Time, string) ([]*domain.MajorEvent, error) {
	return []*domain.MajorEvent{{ID: 1, Title: "공연"}}, nil
}

func (*renderFailureEvents) GetEventsByMonth(context.Context, int, int, string) ([]*domain.MajorEvent, error) {
	return []*domain.MajorEvent{{ID: 1, Title: "공연"}}, nil
}

func (r *renderFailureEvents) MarkEventsAsNotified(context.Context, []int, string) error {
	r.marked = true
	return nil
}

func (r *renderFailureEvents) MarkEventsAsMonthlyNotified(context.Context, []int, string) error {
	r.marked = true
	return nil
}

type renderFailureOutbox struct{ calls int }

func (r *renderFailureOutbox) Enqueue(context.Context, domain.DeliveryOutboxKind, string, string, string) error {
	r.calls++
	return nil
}

type renderFailureLocker struct{}

func (renderFailureLocker) TryAcquire(context.Context, string, time.Duration) (string, bool, error) {
	return "test-token", true, nil
}
func (renderFailureLocker) Release(context.Context, string, string) error { return nil }

// 실제 템플릿의 공백 렌더가 오류로 이어져 enqueue와 알림 완료 표시를 막는지 검증한다.
func TestWhitespaceEventTemplatesDoNotEnqueueOrMark(t *testing.T) {
	for _, key := range []domain.TemplateKey{domain.TemplateKeyCmdMajorEventWeeklySummary, domain.TemplateKeyCmdMajorEventMonthlySummary} {
		t.Run(string(key), func(t *testing.T) {
			f := newLLMSchedulerFormatter("!", setupFormatterRenderer(t, key, " \n\t"), nil, true)
			repository := &renderFailureEvents{}
			outbox := &renderFailureOutbox{}
			guard, err := promptguard.NewGuard(promptguard.Config{Enabled: true, UseEmbeddedDefaults: true}, nil)
			require.NoError(t, err)

			logger := slog.New(slog.DiscardHandler)

			if key == domain.TemplateKeyCmdMajorEventWeeklySummary {
				scheduler := eventscheduler.NewScheduler(repository, f, nil, renderFailureLocker{}, outbox, logger, eventscheduler.WithGuards(guard, outputguard.NewGuard()))

				err = scheduler.SendWeeklyNotification(t.Context())
			} else {
				scheduler := eventscheduler.NewMonthlyScheduler(repository, f, nil, renderFailureLocker{}, outbox, logger, eventscheduler.WithMonthlyGuards(guard, outputguard.NewGuard()))

				err = scheduler.SendMonthlyNotification(t.Context())
			}

			require.ErrorContains(t, err, "template rendered empty")
			require.Zero(t, outbox.calls)
			require.False(t, repository.marked)
		})
	}
}
