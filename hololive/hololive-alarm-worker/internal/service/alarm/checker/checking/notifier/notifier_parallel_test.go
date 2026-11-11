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
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/dedup"
	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/queue"
	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestPrepareSendBatch_BoundedConcurrency(t *testing.T) {
	t.Parallel()

	if prepareBatchConcurrency <= 0 {
		t.Fatalf("prepareBatchConcurrency must be positive, got %d", prepareBatchConcurrency)
	}

	if prepareBatchConcurrency > 16 {
		t.Fatalf("prepareBatchConcurrency too aggressive for Valkey claim path: %d", prepareBatchConcurrency)
	}
}

func TestPrepareSendBatch_PreservesOrderAcrossManyItems(t *testing.T) {
	t.Parallel()

	cache := newCheckerTestCacheClient(t)
	logger := newCheckerTestLogger()
	dedupService := dedup.NewService(cache, logger)
	outbox := &notifierBatchOutbox{}

	notifier, err := NewNotifier(
		dedupService,
		newCheckerTestPublisher(t, cache, outbox, queue.PublishConfig{WakeupEnabled: false, MaxDeliveriesPerBatch: 1000}),
		logger,
	)
	require.NoError(t, err)

	start := time.Now().UTC().Add(10 * time.Minute)

	const total = 64

	notifications := make([]*domain.AlarmNotification, 0, total)
	wantRoomOrder := make([]string, 0, total)

	for i := range total {
		roomID := fmt.Sprintf("room-order-%03d", i)
		stream := &domain.Stream{
			ID:             fmt.Sprintf("stream-order-%03d", i),
			Title:          "Order Test",
			ChannelID:      "UC_ORDER",
			Status:         domain.StreamStatusUpcoming,
			StartScheduled: &start,
			Channel:        &domain.Channel{ID: "UC_ORDER", Name: "Order Channel"},
		}

		notifications = append(notifications, domain.NewAlarmNotification(roomID, stream.Channel, stream, 10, []string{}, ""))
		wantRoomOrder = append(wantRoomOrder, roomID)
	}

	result, sendErr := notifier.Send(t.Context(), notifications, testTargetPolicy(10))
	require.NoError(t, sendErr)
	assert.Equal(t, total, result.Sent)
	assert.Equal(t, 0, result.Skipped)
	assert.Equal(t, 0, result.Failed)

	require.Equal(t, 1, outbox.insertBatchCalls)
	require.Len(t, outbox.lastBatchInput.Envelopes, total)

	gotRoomOrder := make([]string, 0, total)

	for _, envelope := range outbox.lastBatchInput.Envelopes {
		gotRoomOrder = append(gotRoomOrder, envelope.Notification.RoomID)
	}

	assert.Equal(t, wantRoomOrder, gotRoomOrder, "publish order must match input order")
}

func TestPrepareSendBatch_DedupExactlyOnceUnderDuplicates(t *testing.T) {
	t.Parallel()

	cache := newCheckerTestCacheClient(t)
	logger := newCheckerTestLogger()
	dedupService := dedup.NewService(cache, logger)
	outbox := &notifierBatchOutbox{}

	notifier, err := NewNotifier(
		dedupService,
		newCheckerTestPublisher(t, cache, outbox, queue.PublishConfig{WakeupEnabled: false, MaxDeliveriesPerBatch: 1000}),
		logger,
	)
	require.NoError(t, err)

	start := time.Now().UTC().Add(10 * time.Minute)
	stream := &domain.Stream{
		ID:             "stream-dup",
		Title:          "Dup Test",
		ChannelID:      "UC_DUP",
		Status:         domain.StreamStatusUpcoming,
		StartScheduled: &start,
		Channel:        &domain.Channel{ID: "UC_DUP", Name: "Dup Channel"},
	}

	const copies = 32

	notifications := make([]*domain.AlarmNotification, 0, copies)

	for range copies {
		// 동일 room+stream+minute -> 동일 claim key. 정확히 1회만 통과해야 한다.
		notifications = append(notifications, domain.NewAlarmNotification("room-dup", stream.Channel, stream, 10, []string{}, ""))
	}

	result, sendErr := notifier.Send(t.Context(), notifications, testTargetPolicy(10))
	require.NoError(t, sendErr)

	assert.Equal(t, 1, result.Sent, "exactly one duplicate must claim and send")
	assert.Equal(t, copies-1, result.Skipped, "remaining duplicates must be skipped")
	assert.Equal(t, 0, result.Failed)

	require.Equal(t, 1, outbox.insertBatchCalls)
	require.Len(t, outbox.lastBatchInput.Envelopes, 1)
}

func TestSend_CanceledContextReturnsErrorAndSendsNothing(t *testing.T) {
	t.Parallel()

	cache := newCheckerTestCacheClient(t)
	logger := newCheckerTestLogger()
	dedupService := dedup.NewService(cache, logger)
	outbox := &notifierBatchOutbox{}

	notifier, err := NewNotifier(
		dedupService,
		newCheckerTestPublisher(t, cache, outbox, queue.PublishConfig{WakeupEnabled: false, MaxDeliveriesPerBatch: 1000}),
		logger,
	)
	require.NoError(t, err)

	start := time.Date(2026, time.June, 11, 14, 0, 0, 0, time.UTC)
	notifications := make([]*domain.AlarmNotification, 0, 4)

	for i := range 4 {
		roomID := fmt.Sprintf("room-cancel-%d", i)
		stream := &domain.Stream{
			ID:             fmt.Sprintf("stream-cancel-%d", i),
			Title:          "Cancel Test",
			ChannelID:      "UC_CANCEL",
			Status:         domain.StreamStatusUpcoming,
			StartScheduled: &start,
			Channel:        &domain.Channel{ID: "UC_CANCEL", Name: "Cancel Channel"},
		}

		notifications = append(notifications, domain.NewAlarmNotification(roomID, stream.Channel, stream, 10, []string{}, ""))
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel() // 이미 취소된 ctx: 모든 worker 가 결정적으로 취소 분기를 탄다(타이밍 의존 없음).

	result, sendErr := notifier.Send(ctx, notifications, testTargetPolicy(10))

	require.Error(t, sendErr, "canceled context must surface a non-nil error to the caller")
	require.ErrorIs(t, sendErr, context.Canceled, "error must wrap context.Canceled")

	assert.Equal(t, 0, result.Sent, "no notification may be sent under canceled context")
	assert.Equal(t, 0, outbox.insertBatchCalls, "no publish batch may run under canceled context")
}
