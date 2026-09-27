package scheduler

import (
	"context"
	"errors"
	"testing"

	"github.com/park285/shared-go/v2/pkg/llm/openaipreset"
	"github.com/park285/shared-go/v2/pkg/outputguard"

	mesummarizer "github.com/kapu/hololive-api/internal/planes/llm/internal/service/majorevent/summarizer"
)

type failingSummaryLLM struct{}

func (failingSummaryLLM) GenerateJSON(context.Context, openaipreset.PromptLayers, map[string]any) (string, error) {
	return "", errors.New("llm unavailable")
}

// DEC-20260926-hololive-source-fallbacks-retirement: LLM 요약 실패를 빈 요약으로 바꿔 이벤트 목록만 보내지 않는다.
// 요약 실패는 스케줄러 오류이고, 구독 방에는 아무것도 보내지 않으며 이벤트는 다음 실행에서 다시 시도하도록 미표시로 남는다.
func TestSendWeeklyNotification_SummaryFailure_DoesNotEnqueueOrMark(t *testing.T) {
	repository := &mockEventRepository{
		rooms:  testRooms(testRoomID1),
		events: testEvents(1),
	}
	outbox := newMockOutboxRepository()
	scheduler := NewScheduler(
		repository,
		&mockFormatter{message: "event list only"},
		mesummarizer.NewEventSummarizer(failingSummaryLLM{}, nil, nil, testLogger()),
		&mockNotificationLocker{acquireAcquired: true},
		outbox,
		testLogger(),
		WithGuards(newMajorEventPromptGuardForSchedulerTest(), outputguard.NewGuard()),
	)

	if err := scheduler.SendWeeklyNotification(t.Context()); err == nil {
		t.Fatal("SendWeeklyNotification() error = nil, want summary failure")
	}

	if len(outbox.enqueuedItems) != 0 {
		t.Fatalf("enqueued items = %d, want 0", len(outbox.enqueuedItems))
	}

	if repository.markedWeekly {
		t.Fatal("events must stay unmarked after a summary failure")
	}
}

func TestSendMonthlyNotification_SummaryFailure_DoesNotEnqueueOrMark(t *testing.T) {
	repository := &mockEventRepository{
		rooms:         testRooms(testRoomID1),
		monthlyEvents: testEvents(1),
	}
	outbox := newMockOutboxRepository()
	scheduler := NewMonthlyScheduler(
		repository,
		&mockFormatter{message: "event list only"},
		mesummarizer.NewEventSummarizer(failingSummaryLLM{}, nil, nil, testLogger()),
		&mockNotificationLocker{acquireAcquired: true},
		outbox,
		testLogger(),
		WithMonthlyGuards(newMajorEventPromptGuardForSchedulerTest(), outputguard.NewGuard()),
	)

	if err := scheduler.SendMonthlyNotification(t.Context()); err == nil {
		t.Fatal("SendMonthlyNotification() error = nil, want summary failure")
	}

	if len(outbox.enqueuedItems) != 0 {
		t.Fatalf("enqueued items = %d, want 0", len(outbox.enqueuedItems))
	}

	if repository.markedMonthly {
		t.Fatal("events must stay unmarked after a summary failure")
	}
}
