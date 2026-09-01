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

package youtubedispatch

import (
	"log/slog"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/dispatchstate"
)

func TestDispatchDeliveryRows_CapturesSuccessAndFailureBuckets(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	cache, mini := newDispatcherTestCache(t)

	defer mini.Close()
	defer func() {
		if err := cache.Close(); err != nil {
			t.Fatalf("close cache service: %v", err)
		}
	}()

	dispatcher := NewDispatcher(nil, cache, &testSender{
		failRoom: map[string]bool{"room-fail": true},
	}, newSendTestRenderer(t), slog.New(slog.DiscardHandler), &dispatchstate.Config{
		DeliveryParallelism: 1,
	})

	rows := []domain.YouTubeNotificationDelivery{
		{ID: 1, OutboxID: 100, RoomID: "room-ok"},
		{ID: 2, OutboxID: 100, RoomID: "room-fail"},
		{ID: 3, OutboxID: 999, RoomID: "room-missing"},
	}
	outboxByID := map[int64]domain.YouTubeNotificationOutbox{
		100: {
			ID:            100,
			Kind:          domain.OutboxKindNewVideo,
			ChannelID:     "UC_dispatch_delivery",
			ContentID:     "video_dispatch_delivery",
			Payload:       `{"video_id":"vid1","title":"dispatch test"}`,
			Status:        domain.OutboxStatusPending,
			AttemptCount:  0,
			NextAttemptAt: time.Now(),
		},
	}

	result := dispatcher.send.dispatchDeliveryRows(ctx, rows, outboxByID)

	if !reflect.DeepEqual(result.SuccessDeliveryIDs, []int64{1}) {
		t.Fatalf("successDeliveryIDs = %#v, want []int64{1}", result.SuccessDeliveryIDs)
	}

	if result.FailedDeliveries != 2 {
		t.Fatalf("failedDeliveries = %d, want 2", result.FailedDeliveries)
	}

	if !reflect.DeepEqual(result.FailureBuckets[deliveryReasonSendMessage], []int64{2}) {
		t.Fatalf("send message failures = %#v, want []int64{2}", result.FailureBuckets[deliveryReasonSendMessage])
	}

	if !reflect.DeepEqual(result.FailureBuckets["outbox row not found"], []int64{3}) {
		t.Fatalf("outbox row not found failures = %#v, want []int64{3}", result.FailureBuckets["outbox row not found"])
	}

	wantTouched := []int64{100, 100, 999}
	gotTouched := make([]int64, len(result.TouchedOutboxIDs))
	copy(gotTouched, result.TouchedOutboxIDs)
	slices.Sort(gotTouched)
	slices.Sort(wantTouched)

	if !reflect.DeepEqual(gotTouched, wantTouched) {
		t.Fatalf("touchedOutboxIDs (sorted) = %#v, want %#v", gotTouched, wantTouched)
	}
}
