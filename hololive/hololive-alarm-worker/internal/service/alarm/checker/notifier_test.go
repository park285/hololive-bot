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

package checker

import (
	"bytes"
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	sharedchecker "github.com/kapu/hololive-shared/pkg/service/alarm/checker"
	"github.com/kapu/hololive-shared/pkg/service/alarm/dedup"
	"github.com/kapu/hololive-shared/pkg/service/alarm/queue"
	"github.com/kapu/hololive-shared/pkg/service/alarm/tier"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
	"github.com/kapu/hololive-shared/pkg/service/holodex"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/valkey-io/valkey-go"
)

type failingPublishCacheClient struct {
	cache.Client
}

func (c *failingPublishCacheClient) DoMulti(context.Context, ...valkey.Completed) []valkey.ValkeyResult {
	return nil
}

func TestNotifierSend_DedupSkip(t *testing.T) {
	t.Parallel()

	cacheSvc := newCheckerTestCacheClient(t)
	dedupSvc := dedup.NewService(cacheSvc, []int{5, 3, 1}, newCheckerTestLogger())

	notifier, err := NewNotifier(
		dedupSvc,
		queue.NewPublisher(cacheSvc, newCheckerTestLogger()),
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

	if _, claimed, claimErr := dedupSvc.TryClaimNotification(t.Context(), "room1", stream.ID, start, 5); claimErr != nil {
		t.Fatalf("TryClaimNotification() error = %v", claimErr)
	} else if !claimed {
		t.Fatal("expected pre-claim to succeed")
	}

	result, sendErr := notifier.Send(t.Context(), []*domain.AlarmNotification{notification})
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
	dedupSvc := dedup.NewService(cacheSvc, []int{5, 3, 1}, newCheckerTestLogger())

	notifier, err := NewNotifier(
		dedupSvc,
		queue.NewPublisher(cacheSvc, newCheckerTestLogger()),
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

	result, sendErr := notifier.Send(t.Context(), []*domain.AlarmNotification{notification})
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

	startScheduled, err := cacheSvc.HGet(t.Context(), notifiedKey, "start_scheduled")
	if err != nil {
		t.Fatalf("expected hash-based notified cache, got error: %v", err)
	}

	if startScheduled == "" {
		t.Fatal("expected start_scheduled field to be written")
	}

	minuteSent, err := cacheSvc.HGet(t.Context(), notifiedKey, "5")
	if err != nil {
		t.Fatalf("expected minute field to be readable from hash: %v", err)
	}

	if minuteSent != "1" {
		t.Fatalf("expected minute field to be 1, got %q", minuteSent)
	}
}

func TestNotifierSend_ReleasesScheduleChangeClaimsOnPublishFailure(t *testing.T) {
	t.Parallel()

	cacheSvc := newCheckerTestCacheClient(t)
	failingCache := &failingPublishCacheClient{Client: cacheSvc}
	logger := newCheckerTestLogger()
	dedupSvc := dedup.NewService(failingCache, []int{5, 3, 1}, logger)
	tierSched := tier.NewTieredScheduler(logger)
	holodexSvc, err := holodex.NewHolodexService("http://unused", "k", failingCache, nil, logger)
	require.NoError(t, err)

	checker, err := NewYouTubeChecker(failingCache, holodexSvc, tierSched, dedupSvc, []int{5, 3, 1}, 0, logger)
	require.NoError(t, err)
	notifier, err := NewNotifier(
		dedupSvc,
		queue.NewPublisher(failingCache, logger),
		tierSched,
		logger,
	)
	require.NoError(t, err)

	previousScheduled := time.Date(2026, 4, 9, 12, 0, 0, 0, time.UTC)
	currentScheduled := time.Date(2026, 4, 9, 12, 2, 0, 0, time.UTC)
	require.NoError(t, dedupSvc.MarkAsNotified(t.Context(), "delayed-publish-fail", previousScheduled, 5))

	window := sharedchecker.EvaluationWindow{
		Start: time.Date(2026, 4, 9, 11, 52, 50, 0, time.UTC),
		End:   time.Date(2026, 4, 9, 11, 53, 10, 0, time.UTC),
	}
	stream := &domain.Stream{
		ID:             "delayed-publish-fail",
		Title:          "publish fail retry",
		ChannelID:      "ch-1",
		Status:         domain.StreamStatusUpcoming,
		StartScheduled: &currentScheduled,
		Channel:        &domain.Channel{ID: "ch-1", Name: "Channel 1"},
	}

	notifications, err := checker.buildUpcomingNotifications(t.Context(), stream, []string{"room-1"}, window)
	require.NoError(t, err)
	require.Len(t, notifications, 1)
	assert.Equal(t, "일정이 늦춰졌습니다.", notifications[0].ScheduleChangeMessage)

	_, sendErr := notifier.Send(t.Context(), notifications)
	require.Error(t, sendErr)

	retryNotifications, err := checker.buildUpcomingNotifications(t.Context(), stream, []string{"room-1"}, window)
	require.NoError(t, err)
	require.Len(t, retryNotifications, 1)
	assert.Equal(t, "일정이 늦춰졌습니다.", retryNotifications[0].ScheduleChangeMessage)
}

func TestNotifierSend_RejectsContentAlarmTypes(t *testing.T) {
	t.Parallel()

	cacheSvc := cachemocks.NewStrictClient()
	logBuffer := &bytes.Buffer{}
	logger := slog.New(slog.NewJSONHandler(logBuffer, &slog.HandlerOptions{Level: slog.LevelInfo}))
	dedupSvc := dedup.NewService(cacheSvc, []int{5, 3, 1}, logger)

	notifier, err := NewNotifier(
		dedupSvc,
		queue.NewPublisher(cacheSvc, logger),
		nil,
		logger,
	)
	if err != nil {
		t.Fatalf("NewNotifier() error = %v", err)
	}

	start := time.Now().UTC().Add(5 * time.Minute).Truncate(time.Minute)
	for _, alarmType := range []domain.AlarmType{domain.AlarmTypeCommunity, domain.AlarmTypeShorts} {
		t.Run(string(alarmType), func(t *testing.T) {
			stream := &domain.Stream{
				ID:             "blocked-" + string(alarmType),
				Title:          "blocked route",
				ChannelID:      "UC_BLOCKED",
				Status:         domain.StreamStatusUpcoming,
				StartScheduled: &start,
				Channel:        &domain.Channel{ID: "UC_BLOCKED", Name: "blocked"},
			}
			notification := domain.NewAlarmNotification("room-blocked", stream.Channel, stream, 5, []string{}, "")
			notification.AlarmType = alarmType
			logBuffer.Reset()

			result, sendErr := notifier.Send(t.Context(), []*domain.AlarmNotification{notification})
			require.Error(t, sendErr)
			assert.Contains(t, sendErr.Error(), "youtube outbox path")
			assert.Equal(t, SendResult{}, result)
			assert.Contains(t, logBuffer.String(), legacyCommunityShortsRouteAuditLogMessage)
			assert.Contains(t, logBuffer.String(), "\"delivery_path\":\""+legacyCommunityShortsDeliveryPath+"\"")
			assert.Contains(t, logBuffer.String(), "\"alarm_type\":\""+string(alarmType)+"\"")
		})
	}
}

func TestNotifierSend_BatchContinuesAfterPublish(t *testing.T) {
	t.Parallel()

	cacheSvc := newCheckerTestCacheClient(t)
	logger := newCheckerTestLogger()
	dedupSvc := dedup.NewService(cacheSvc, []int{5, 3, 1}, logger)

	notifier, err := NewNotifier(
		dedupSvc,
		queue.NewPublisher(cacheSvc, logger),
		tier.NewTieredScheduler(logger),
		logger,
	)
	if err != nil {
		t.Fatalf("NewNotifier() error = %v", err)
	}

	start := time.Now().UTC().Add(5 * time.Minute).Truncate(time.Minute)
	makeNotification := func(roomID, streamID string) *domain.AlarmNotification {
		stream := &domain.Stream{
			ID:             streamID,
			Title:          "Batch Test " + streamID,
			ChannelID:      "UC_BATCH",
			Status:         domain.StreamStatusUpcoming,
			StartScheduled: &start,
			Channel:        &domain.Channel{ID: "UC_BATCH", Name: "Batch Channel"},
		}
		return domain.NewAlarmNotification(roomID, stream.Channel, stream, 5, []string{}, "")
	}

	notifications := []*domain.AlarmNotification{
		makeNotification("room-batch-1", "stream-batch-1"),
		makeNotification("room-batch-2", "stream-batch-2"),
	}

	result, sendErr := notifier.Send(t.Context(), notifications)
	if sendErr != nil {
		t.Fatalf("Send() error = %v", sendErr)
	}

	if result.Sent != 2 {
		t.Fatalf("expected Sent=2, got %d (Skipped=%d, Failed=%d)", result.Sent, result.Skipped, result.Failed)
	}

	if queueSize := readDispatchQueueSize(t, cacheSvc); queueSize != 2 {
		t.Fatalf("expected dispatch queue size=2, got %d", queueSize)
	}
}

func readDispatchQueueSize(t *testing.T, cacheSvc cache.Client) int64 {
	t.Helper()

	resp := cacheSvc.GetClient().Do(t.Context(), cacheSvc.B().Llen().Key(queue.AlarmDispatchQueue).Build())

	size, err := resp.AsInt64()
	if err != nil {
		t.Fatalf("queue size lookup failed: %v", err)
	}

	return size
}
