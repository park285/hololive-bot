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
	"errors"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/alarm/dedup"
	"github.com/kapu/hololive-shared/pkg/service/alarm/dispatchoutbox"
	"github.com/kapu/hololive-shared/pkg/service/alarm/queue"
	"github.com/kapu/hololive-shared/pkg/service/alarm/tier"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	assert.Zero(t, readDispatchQueueSize(t, cache))
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
	assert.Equal(t, dispatchoutbox.StatusPending, outbox.lastBatchInput.Status)
	assert.Zero(t, readDispatchQueueSize(t, cache))

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
	firstClaimKey, firstClaimed, err := dedupService.TryClaimNotification(t.Context(), "room-partial-1", "stream-partial-1", start, 10)
	require.NoError(t, err)
	require.True(t, firstClaimed)
	secondClaimKey, secondClaimed, err := dedupService.TryClaimNotification(t.Context(), "room-partial-2", "stream-partial-2", start, 10)
	require.NoError(t, err)
	require.True(t, secondClaimed)

	items := []claimedSend{
		newNotifierPublishTestItem("room-partial-1", "stream-partial-1", "channel-partial", start, 10, []string{firstClaimKey}),
		newNotifierPublishTestItem("room-partial-2", "stream-partial-2", "channel-partial", start, 10, []string{secondClaimKey}),
	}

	processed, err := notifier.publishBatchAndMark(t.Context(), items)
	require.Error(t, err)
	assert.Equal(t, 1, processed)
	assert.ErrorContains(t, err, "publish queue batch")
	assert.Equal(t, 2, outbox.insertBatchCalls)

	_, firstClaimedAgain, err := dedupService.TryClaimNotification(t.Context(), "room-partial-1", "stream-partial-1", start, 10)
	require.NoError(t, err)
	assert.False(t, firstClaimedAgain)

	_, secondClaimedAgain, err := dedupService.TryClaimNotification(t.Context(), "room-partial-2", "stream-partial-2", start, 10)
	require.NoError(t, err)
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
