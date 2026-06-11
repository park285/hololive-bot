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

// 병렬 prepare 가 보수적 동시성 한도로 묶여 있어야 한다(Valkey 부하 제어).
func TestPrepareSendBatch_BoundedConcurrency(t *testing.T) {
	t.Parallel()

	if prepareBatchConcurrency <= 0 {
		t.Fatalf("prepareBatchConcurrency must be positive, got %d", prepareBatchConcurrency)
	}
	if prepareBatchConcurrency > 16 {
		t.Fatalf("prepareBatchConcurrency too aggressive for Valkey claim path: %d", prepareBatchConcurrency)
	}
}

// 다수 항목을 병렬 prepare 해도 publish envelope 순서가 입력 순서대로 보존돼야 한다(누락 금지·순서 안정성).
func TestPrepareSendBatch_PreservesOrderAcrossManyItems(t *testing.T) {
	t.Parallel()

	cache := newCheckerTestCacheClient(t)
	logger := newCheckerTestLogger()
	dedupService := dedup.NewService(cache, []int{10}, logger)
	outbox := &notifierBatchOutbox{}

	notifier, err := NewNotifier(
		dedupService,
		queue.NewPublisher(cache, logger,
			queue.WithOutbox(outbox),
			queue.WithPublishMode(queue.PublishModePGFirst),
			queue.WithWakeupEnabled(false),
		),
		tier.NewTieredScheduler(logger),
		logger,
	)
	require.NoError(t, err)

	start := time.Date(2026, 6, 11, 12, 10, 0, 0, time.UTC)
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

	result, sendErr := notifier.Send(t.Context(), notifications)
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

// 동일 (room, stream) 중복 항목은 병렬 claim 경합에서도 정확히 1회만 통과해야 한다(중복 발송 금지).
func TestPrepareSendBatch_DedupExactlyOnceUnderDuplicates(t *testing.T) {
	t.Parallel()

	cache := newCheckerTestCacheClient(t)
	logger := newCheckerTestLogger()
	dedupService := dedup.NewService(cache, []int{10}, logger)
	outbox := &notifierBatchOutbox{}

	notifier, err := NewNotifier(
		dedupService,
		queue.NewPublisher(cache, logger,
			queue.WithOutbox(outbox),
			queue.WithPublishMode(queue.PublishModePGFirst),
			queue.WithWakeupEnabled(false),
		),
		tier.NewTieredScheduler(logger),
		logger,
	)
	require.NoError(t, err)

	start := time.Date(2026, 6, 11, 13, 0, 0, 0, time.UTC)
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

	result, sendErr := notifier.Send(t.Context(), notifications)
	require.NoError(t, sendErr)

	assert.Equal(t, 1, result.Sent, "exactly one duplicate must claim and send")
	assert.Equal(t, copies-1, result.Skipped, "remaining duplicates must be skipped")
	assert.Equal(t, 0, result.Failed)

	require.Equal(t, 1, outbox.insertBatchCalls)
	require.Len(t, outbox.lastBatchInput.Envelopes, 1)
}

// ctx 가 이미 취소된 상태에서 Send 는 직렬 base 와 동등하게 non-nil error 를 반환해야 한다.
// (caller 가 EventAlarmNotificationDispatchFailed 로 기록하도록) — 발송 누락을 success 로 오기록하면 안 된다.
func TestSend_CanceledContextReturnsErrorAndSendsNothing(t *testing.T) {
	t.Parallel()

	cache := newCheckerTestCacheClient(t)
	logger := newCheckerTestLogger()
	dedupService := dedup.NewService(cache, []int{10}, logger)
	outbox := &notifierBatchOutbox{}

	notifier, err := NewNotifier(
		dedupService,
		queue.NewPublisher(cache, logger,
			queue.WithOutbox(outbox),
			queue.WithPublishMode(queue.PublishModePGFirst),
			queue.WithWakeupEnabled(false),
		),
		tier.NewTieredScheduler(logger),
		logger,
	)
	require.NoError(t, err)

	start := time.Date(2026, 6, 11, 14, 0, 0, 0, time.UTC)
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

	result, sendErr := notifier.Send(ctx, notifications)

	require.Error(t, sendErr, "canceled context must surface a non-nil error to the caller")
	assert.True(t, errors.Is(sendErr, context.Canceled), "error must wrap context.Canceled")

	assert.Equal(t, 0, result.Sent, "no notification may be sent under canceled context")
	assert.Equal(t, 0, outbox.insertBatchCalls, "no publish batch may run under canceled context")
}

var _ = dispatchoutbox.Inserted
