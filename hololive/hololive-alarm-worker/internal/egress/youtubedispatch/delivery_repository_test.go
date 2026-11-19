package youtubedispatch

import (
	"log/slog"
	"reflect"
	"slices"
	"testing"
	"time"

	ytlifecycle "github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/lifecycle"
	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/store"
	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestDispatchDeliveryRows_CapturesSuccessAndFailures(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	cache, mini := newDispatcherTestCache(t)

	defer mini.Close()
	defer func() {
		if err := cache.Close(); err != nil {
			t.Fatalf("close cache service: %v", err)
		}
	}()

	dispatcher := newDispatcherForTest(t, nil, cache, &testSender{
		failRoom: map[string]bool{"room-fail": true},
	}, newSendTestRenderer(t), slog.New(slog.DiscardHandler), &Config{
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

	wantStarted := []string{startedFailureRecord(ytlifecycle.FailureRetryable, lifecycleReasonRateLimited, 0, store.DeliveryModePerRoom)}
	if got := transitionSpy(t, dispatcher).recordedStartedFailures(); !slices.Equal(got, wantStarted) {
		t.Fatalf("started failures = %v, want %v", got, wantStarted)
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
