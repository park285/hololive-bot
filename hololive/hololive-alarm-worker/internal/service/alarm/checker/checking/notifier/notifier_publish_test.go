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

package notifier

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/dedup"
	alarmkeys "github.com/kapu/hololive-alarm-worker/internal/service/alarm/keys"
	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/queue"
	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/tier"
	"github.com/kapu/hololive-shared/pkg/domain"
	sharedcache "github.com/kapu/hololive-shared/pkg/service/cache"
)

func TestNotifierPublishBatchAndMarkEmpty(t *testing.T) {
	t.Parallel()

	cache := newCheckerTestCacheClient(t)
	logger := newCheckerTestLogger()
	notifier, err := NewNotifier(
		dedup.NewService(cache, []int{10}, logger),
		queue.NewPublisher(cache, logger),
		tier.NewTieredScheduler(logger),
		logger,
	)
	require.NoError(t, err)

	processed, err := notifier.publishBatchAndMark(t.Context(), nil)
	require.NoError(t, err)
	assert.Zero(t, processed)
}

func TestNotifierPublishBatchAndMarkSuccess(t *testing.T) {
	t.Parallel()

	cache := newCheckerTestCacheClient(t)
	logger := newCheckerTestLogger()
	dedupService := dedup.NewService(cache, []int{10}, logger)
	outbox := &notifierBatchOutbox{}
	notifier, err := NewNotifier(
		dedupService,
		queue.NewPublisher(cache, logger,
			queue.WithOutbox(outbox),
			queue.WithWakeupEnabled(false),
		),
		tier.NewTieredScheduler(logger),
		logger,
	)
	require.NoError(t, err)

	start := time.Date(2026, time.May, 24, 10, 10, 0, 0, time.UTC)
	item := newNotifierPublishTestItem("room-success", "stream-success", "channel-success", start, 10, []string{"claim-success"})

	processed, err := notifier.publishBatchAndMark(t.Context(), []claimedSend{item})
	require.NoError(t, err)
	assert.Equal(t, 1, processed)
	assert.Equal(t, 1, outbox.insertBatchCalls)

	alreadyNotified, err := dedupService.IsAlreadyNotifiedForSchedule(t.Context(), "stream-success", start, 10)
	require.NoError(t, err)
	assert.True(t, alreadyNotified)

	upcomingNotified, err := dedupService.WasUpcomingEventNotifiedRecently(
		t.Context(),
		"room-success",
		"channel-success",
		item.payload.notification.Stream,
		time.Hour,
	)
	require.NoError(t, err)
	assert.True(t, upcomingNotified)
}

func TestNotifierPublishBatchAndMarkErrorReleasesUnprocessedClaims(t *testing.T) {
	t.Parallel()

	cache := newCheckerTestCacheClient(t)
	logger := newCheckerTestLogger()
	dedupService := dedup.NewService(cache, []int{10}, logger)
	outbox := &notifierBatchOutbox{batchErrors: []error{nil, errors.New("pg unavailable")}}
	notifier, err := NewNotifier(
		dedupService,
		queue.NewPublisher(cache, logger,
			queue.WithOutbox(outbox),
			queue.WithWakeupEnabled(false),
			queue.WithMaxDeliveriesPerBatch(1),
		),
		tier.NewTieredScheduler(logger),
		logger,
	)
	require.NoError(t, err)

	start := time.Date(2026, time.May, 24, 10, 10, 0, 0, time.UTC)
	items := []claimedSend{
		newNotifierPublishTestItem("room-partial-1", "stream-partial-1", "channel-partial", start, 10, nil),
		newNotifierPublishTestItem("room-partial-2", "stream-partial-2", "channel-partial", start, 10, nil),
	}

	for i := range items {
		claimKey, claimed := claimNotificationPair(t, notifier, items[i].payload)
		require.True(t, claimed)

		items[i].claimKeys = []string{claimKey}
	}

	processed, err := notifier.publishBatchAndMark(t.Context(), items)
	require.Error(t, err)
	assert.Equal(t, 1, processed)
	require.ErrorContains(t, err, "publish queue batch")
	assert.Equal(t, 2, outbox.insertBatchCalls)

	_, firstClaimedAgain := claimNotificationPair(t, notifier, items[0].payload)
	assert.False(t, firstClaimedAgain)

	_, secondClaimedAgain := claimNotificationPair(t, notifier, items[1].payload)
	assert.True(t, secondClaimedAgain)

	firstNotified, err := dedupService.IsAlreadyNotifiedForSchedule(t.Context(), "stream-partial-1", start, 10)
	require.NoError(t, err)
	assert.True(t, firstNotified)

	secondNotified, err := dedupService.IsAlreadyNotifiedForSchedule(t.Context(), "stream-partial-2", start, 10)
	require.NoError(t, err)
	assert.False(t, secondNotified)
}

func newNotifierPublishTestItem(
	roomID string,
	streamID string,
	channelID string,
	start time.Time,
	minutesUntil int,
	claimKeys []string,
) claimedSend {
	stream := &domain.Stream{
		ID:             streamID,
		Title:          "Publish " + streamID,
		ChannelID:      channelID,
		Status:         domain.StreamStatusUpcoming,
		StartScheduled: &start,
		Channel:        &domain.Channel{ID: channelID, Name: "Publish Channel"},
	}
	notification := domain.NewAlarmNotification(roomID, stream.Channel, stream, minutesUntil, []string{}, "")

	return claimedSend{
		payload: &sendInput{
			notification:   notification,
			streamID:       streamID,
			channelID:      channelID,
			startScheduled: start,
		},
		claimKeys: claimKeys,
	}
}

// 같은 stream을 여러 방에 발행해도 stream 단위 notified hash는 한 번만 쓰고, 방별 upcoming 표식은 방마다 남긴다.
func TestNotifierPublishBatchAndMarkWritesStreamMarkOncePerSchedule(t *testing.T) {
	t.Parallel()

	cache := &hmsetCountingCache{Client: newCheckerTestCacheClient(t), calls: map[string]int{}}
	logger := newCheckerTestLogger()
	dedupService := dedup.NewService(cache, []int{10}, logger)
	notifier, err := NewNotifier(
		dedupService,
		queue.NewPublisher(cache, logger,
			queue.WithOutbox(&notifierBatchOutbox{}),
			queue.WithWakeupEnabled(false),
		),
		tier.NewTieredScheduler(logger),
		logger,
	)
	require.NoError(t, err)

	start := time.Date(2026, time.May, 24, 10, 10, 0, 0, time.UTC)
	items := []claimedSend{
		newNotifierPublishTestItem("room-a", "stream-shared", "channel-shared", start, 10, []string{"claim-a"}),
		newNotifierPublishTestItem("room-b", "stream-shared", "channel-shared", start, 10, []string{"claim-b"}),
	}

	processed, err := notifier.publishBatchAndMark(t.Context(), items)
	require.NoError(t, err)
	assert.Equal(t, 2, processed)
	assert.Equal(t, 1, cache.hmsetCalls(alarmkeys.NotifiedKey("stream-shared")))

	for _, room := range []string{"room-a", "room-b"} {
		upcomingNotified, err := dedupService.WasUpcomingEventNotifiedRecently(
			t.Context(), room, "channel-shared", items[0].payload.notification.Stream, time.Hour)
		require.NoError(t, err)
		assert.True(t, upcomingNotified, room)
	}
}

// 같은 stream에 다른 예정 시각이 끼면 MarkAsNotified가 hash를 비우므로, 앞서 기록한 예정 시각도 다시 써서 마지막
// 발행 순서의 예정 시각이 남아야 한다.
func TestNotifierPublishBatchAndMarkRewritesStreamMarkAfterScheduleChange(t *testing.T) {
	t.Parallel()

	cache := &hmsetCountingCache{Client: newCheckerTestCacheClient(t), calls: map[string]int{}}
	logger := newCheckerTestLogger()
	dedupService := dedup.NewService(cache, []int{10}, logger)
	notifier, err := NewNotifier(
		dedupService,
		queue.NewPublisher(cache, logger,
			queue.WithOutbox(&notifierBatchOutbox{}),
			queue.WithWakeupEnabled(false),
		),
		tier.NewTieredScheduler(logger),
		logger,
	)
	require.NoError(t, err)

	start := time.Date(2026, time.May, 24, 10, 10, 0, 0, time.UTC)
	moved := start.Add(30 * time.Minute)
	items := []claimedSend{
		newNotifierPublishTestItem("room-a", "stream-moved", "channel-moved", start, 10, []string{"claim-a"}),
		newNotifierPublishTestItem("room-b", "stream-moved", "channel-moved", moved, 10, []string{"claim-b"}),
		newNotifierPublishTestItem("room-c", "stream-moved", "channel-moved", start, 10, []string{"claim-c"}),
	}

	processed, err := notifier.publishBatchAndMark(t.Context(), items)
	require.NoError(t, err)
	assert.Equal(t, 3, processed)
	assert.Equal(t, 3, cache.hmsetCalls(alarmkeys.NotifiedKey("stream-moved")))

	notified, err := dedupService.IsAlreadyNotifiedForSchedule(t.Context(), "stream-moved", start, 10)
	require.NoError(t, err)
	assert.True(t, notified)
}

type hmsetCountingCache struct {
	sharedcache.Client

	mu    sync.Mutex
	calls map[string]int
}

func (c *hmsetCountingCache) HMSet(ctx context.Context, key string, values map[string]any) error {
	c.mu.Lock()

	c.calls[key]++
	c.mu.Unlock()

	if err := c.Client.HMSet(ctx, key, values); err != nil {
		return fmt.Errorf("count hmset: %w", err)
	}

	return nil
}

func (c *hmsetCountingCache) hmsetCalls(key string) int {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.calls[key]
}
