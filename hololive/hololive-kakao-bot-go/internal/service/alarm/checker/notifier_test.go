package checker

import (
	"context"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/alarm/dedup"
	"github.com/kapu/hololive-shared/pkg/service/alarm/queue"
	"github.com/kapu/hololive-shared/pkg/service/alarm/tier"
	"github.com/kapu/hololive-shared/pkg/service/cache"
)

func TestNotifierSend_DedupSkip(t *testing.T) {
	t.Parallel()

	cacheSvc := newCheckerTestCacheClient(t)
	alarmSvc := newCheckerTestAlarmService(t, cacheSvc)

	dedupSvc := dedup.NewService(cacheSvc, []int{5, 3, 1}, newCheckerTestLogger())
	notifier, err := NewNotifier(
		dedupSvc,
		queue.NewPublisher(cacheSvc, newCheckerTestLogger()),
		alarmSvc,
		tier.NewTieredScheduler(newCheckerTestLogger()),
		newCheckerTestLogger(),
	)
	if err != nil {
		t.Fatalf("NewNotifier() error = %v", err)
	}

	start := time.Now().UTC().Add(5 * time.Minute).Truncate(time.Minute)
	stream := &domain.Stream{
		ID:             "youtube-stream-1",
		Title:          "테스트 방송",
		ChannelID:      "UC_TEST",
		Status:         domain.StreamStatusUpcoming,
		StartScheduled: &start,
		Channel:        &domain.Channel{ID: "UC_TEST", Name: "테스트 채널"},
	}
	notification := domain.NewAlarmNotification("room1", stream.Channel, stream, 5, []string{}, "")

	if _, claimed, claimErr := dedupSvc.TryClaimNotification(context.Background(), "room1", stream.ID, start, 5); claimErr != nil {
		t.Fatalf("TryClaimNotification() error = %v", claimErr)
	} else if !claimed {
		t.Fatalf("expected pre-claim to succeed")
	}

	result, sendErr := notifier.Send(context.Background(), []*domain.AlarmNotification{notification})
	if sendErr != nil {
		t.Fatalf("Send() error = %v", sendErr)
	}

	if result.Sent != 0 || result.Skipped != 1 || result.Failed != 0 {
		t.Fatalf("unexpected result: %+v", result)
	}

	if queueSize := readDispatchQueueSize(t, cacheSvc); queueSize != 0 {
		t.Fatalf("expected empty dispatch queue, got %d", queueSize)
	}
}

func TestNotifierSend_PublishQueuePath(t *testing.T) {
	t.Parallel()

	cacheSvc := newCheckerTestCacheClient(t)
	alarmSvc := newCheckerTestAlarmService(t, cacheSvc)

	dedupSvc := dedup.NewService(cacheSvc, []int{5, 3, 1}, newCheckerTestLogger())
	notifier, err := NewNotifier(
		dedupSvc,
		queue.NewPublisher(cacheSvc, newCheckerTestLogger()),
		alarmSvc,
		tier.NewTieredScheduler(newCheckerTestLogger()),
		newCheckerTestLogger(),
	)
	if err != nil {
		t.Fatalf("NewNotifier() error = %v", err)
	}

	start := time.Now().UTC().Add(5 * time.Minute).Truncate(time.Minute)
	stream := &domain.Stream{
		ID:             "youtube-stream-2",
		Title:          "테스트 방송 2",
		ChannelID:      "UC_TEST_2",
		Status:         domain.StreamStatusUpcoming,
		StartScheduled: &start,
		Channel:        &domain.Channel{ID: "UC_TEST_2", Name: "테스트 채널 2"},
	}
	notification := domain.NewAlarmNotification("room2", stream.Channel, stream, 5, []string{}, "")

	result, sendErr := notifier.Send(context.Background(), []*domain.AlarmNotification{notification})
	if sendErr != nil {
		t.Fatalf("Send() error = %v", sendErr)
	}

	if result.Sent != 1 || result.Skipped != 0 || result.Failed != 0 {
		t.Fatalf("unexpected result: %+v", result)
	}

	if queueSize := readDispatchQueueSize(t, cacheSvc); queueSize != 1 {
		t.Fatalf("expected dispatch queue size=1, got %d", queueSize)
	}

	notifiedKey := "notified:" + stream.ID
	startScheduled, err := cacheSvc.HGet(context.Background(), notifiedKey, "start_scheduled")
	if err != nil {
		t.Fatalf("expected hash-based notified cache, got error: %v", err)
	}
	if startScheduled == "" {
		t.Fatalf("expected start_scheduled field to be written")
	}

	minuteSent, err := cacheSvc.HGet(context.Background(), notifiedKey, "5")
	if err != nil {
		t.Fatalf("expected minute field to be readable from hash: %v", err)
	}
	if minuteSent != "1" {
		t.Fatalf("expected minute field to be 1, got %q", minuteSent)
	}
}

func readDispatchQueueSize(t *testing.T, cacheSvc cache.Client) int64 {
	t.Helper()

	resp := cacheSvc.GetClient().Do(context.Background(), cacheSvc.B().Llen().Key(queue.AlarmDispatchQueue).Build())
	size, err := resp.AsInt64()
	if err != nil {
		t.Fatalf("queue size lookup failed: %v", err)
	}
	return size
}
