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

	if _, err := scheduler.SendWeeklyNotification(t.Context()); err == nil {
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

	if _, err := scheduler.SendMonthlyNotification(t.Context()); err == nil {
		t.Fatal("SendMonthlyNotification() error = nil, want summary failure")
	}

	if len(outbox.enqueuedItems) != 0 {
		t.Fatalf("enqueued items = %d, want 0", len(outbox.enqueuedItems))
	}

	if repository.markedMonthly {
		t.Fatal("events must stay unmarked after a summary failure")
	}
}
