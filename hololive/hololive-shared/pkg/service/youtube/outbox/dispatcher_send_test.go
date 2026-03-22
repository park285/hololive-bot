package outbox

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func newTestDispatcherForSend(t *testing.T, sender *testSender) *Dispatcher {
	t.Helper()

	cacheSvc, mini := newDispatcherTestCache(t)
	t.Cleanup(func() {
		mini.Close()
		_ = cacheSvc.Close()
	})

	return NewDispatcher(nil, cacheSvc, sender, nil, slog.New(slog.NewTextHandler(io.Discard, nil)), Config{
		BatchSize:           10,
		LockTimeout:         time.Minute,
		PollInterval:        time.Second,
		MaxRetries:          3,
		RetryBackoff:        time.Minute,
		DeliveryParallelism: 2,
	})
}

func TestGroupDeliveryRows(t *testing.T) {
	t.Parallel()

	outboxByID := map[int64]domain.YouTubeNotificationOutbox{
		1: {ID: 1, ChannelID: "UCch1", Kind: domain.OutboxKindNewShort, Payload: `{"video_id":"s1","title":"쇼츠1"}`},
		2: {ID: 2, ChannelID: "UCch1", Kind: domain.OutboxKindNewShort, Payload: `{"video_id":"s2","title":"쇼츠2"}`},
		3: {ID: 3, ChannelID: "UCch1", Kind: domain.OutboxKindNewVideo, Payload: `{"video_id":"v1","title":"영상1"}`},
		4: {ID: 4, ChannelID: "UCch2", Kind: domain.OutboxKindNewShort, Payload: `{"video_id":"s3","title":"쇼츠3"}`},
		5: {ID: 5, ChannelID: "UCch1", Kind: domain.OutboxKindMilestone, Payload: `{"milestone":"100만"}`},
		6: {ID: 6, ChannelID: "UCch1", Kind: domain.OutboxKindMilestone, Payload: `{"milestone":"200만"}`},
	}

	rows := []domain.YouTubeNotificationDelivery{
		{ID: 101, OutboxID: 1, RoomID: "room1"},
		{ID: 102, OutboxID: 2, RoomID: "room1"},
		{ID: 103, OutboxID: 3, RoomID: "room1"},
		{ID: 104, OutboxID: 4, RoomID: "room1"},
		{ID: 105, OutboxID: 1, RoomID: "room2"},
		{ID: 106, OutboxID: 99, RoomID: "room1"},
		{ID: 107, OutboxID: 5, RoomID: "room1"},
		{ID: 108, OutboxID: 6, RoomID: "room1"},
	}

	groups, orphans := groupDeliveryRows(rows, outboxByID)

	if len(orphans) != 1 || orphans[0].ID != 106 {
		t.Fatalf("orphans = %+v, want [{ID:106}]", orphans)
	}

	if len(groups) != 6 {
		t.Fatalf("group count = %d, want 6", len(groups))
	}

	var shortsGroup *deliveryGroup
	for i := range groups {
		if groups[i].roomID == "room1" && groups[i].channelID == "UCch1" && groups[i].kind == domain.OutboxKindNewShort {
			shortsGroup = &groups[i]
			break
		}
	}
	if shortsGroup == nil {
		t.Fatalf("shorts group for room1+UCch1 not found")
	}
	if len(shortsGroup.rows) != 2 {
		t.Fatalf("shorts group row count = %d, want 2", len(shortsGroup.rows))
	}
	if len(shortsGroup.outboxes) != 2 {
		t.Fatalf("shorts group outbox count = %d, want 2", len(shortsGroup.outboxes))
	}

	milestoneCount := 0
	for _, g := range groups {
		if g.kind == domain.OutboxKindMilestone {
			milestoneCount++
			if len(g.rows) != 1 {
				t.Fatalf("milestone group should be single-item, got %d rows", len(g.rows))
			}
		}
	}
	if milestoneCount != 2 {
		t.Fatalf("milestone group count = %d, want 2", milestoneCount)
	}
}

func TestValidateOutboxPayload(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		item   domain.YouTubeNotificationOutbox
		wantOK bool
	}{
		{
			name:   "valid video",
			item:   domain.YouTubeNotificationOutbox{Kind: domain.OutboxKindNewVideo, Payload: `{"video_id":"v1","title":"t"}`},
			wantOK: true,
		},
		{
			name:   "valid short",
			item:   domain.YouTubeNotificationOutbox{Kind: domain.OutboxKindNewShort, Payload: `{"video_id":"s1","title":"t"}`},
			wantOK: true,
		},
		{
			name:   "valid community",
			item:   domain.YouTubeNotificationOutbox{Kind: domain.OutboxKindCommunityPost, Payload: `{"post_id":"p1","content_text":"c"}`},
			wantOK: true,
		},
		{
			name:   "invalid json",
			item:   domain.YouTubeNotificationOutbox{Kind: domain.OutboxKindNewVideo, Payload: `{broken`},
			wantOK: false,
		},
		{
			name:   "milestone always valid",
			item:   domain.YouTubeNotificationOutbox{Kind: domain.OutboxKindMilestone, Payload: `{"milestone":"100만"}`},
			wantOK: true,
		},
		{
			name:   "unknown kind",
			item:   domain.YouTubeNotificationOutbox{Kind: domain.OutboxKind("UNKNOWN"), Payload: `{}`},
			wantOK: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := validateOutboxPayload(tt.item); got != tt.wantOK {
				t.Fatalf("validateOutboxPayload() = %v, want %v", got, tt.wantOK)
			}
		})
	}
}

func TestDispatchDeliveryRows_GroupedFallback(t *testing.T) {
	t.Parallel()

	sender := &testSender{failRoom: map[string]bool{}}
	d := newTestDispatcherForSend(t, sender)

	outboxByID := map[int64]domain.YouTubeNotificationOutbox{
		1: {ID: 1, ChannelID: "UCch1", Kind: domain.OutboxKindNewShort, Payload: `{"video_id":"s1","title":"쇼츠1"}`},
		2: {ID: 2, ChannelID: "UCch1", Kind: domain.OutboxKindNewShort, Payload: `{"video_id":"s2","title":"쇼츠2"}`},
	}

	rows := []domain.YouTubeNotificationDelivery{
		{ID: 101, OutboxID: 1, RoomID: "room1"},
		{ID: 102, OutboxID: 2, RoomID: "room1"},
	}

	result := d.dispatchDeliveryRows(context.Background(), rows, outboxByID)

	if len(result.successDeliveryIDs) != 2 {
		t.Fatalf("successDeliveryIDs = %d, want 2", len(result.successDeliveryIDs))
	}

	sender.mu.Lock()
	msgCount := len(sender.messages)
	sender.mu.Unlock()
	if msgCount != 2 {
		t.Fatalf("sender message count = %d, want 2 (fallback)", msgCount)
	}
}

func TestDispatchDeliveryRows_OrphanRows(t *testing.T) {
	t.Parallel()

	sender := &testSender{failRoom: map[string]bool{}}
	d := newTestDispatcherForSend(t, sender)

	rows := []domain.YouTubeNotificationDelivery{
		{ID: 101, OutboxID: 99, RoomID: "room1"},
	}

	result := d.dispatchDeliveryRows(context.Background(), rows, map[int64]domain.YouTubeNotificationOutbox{})

	if result.failedDeliveries != 1 {
		t.Fatalf("failedDeliveries = %d, want 1", result.failedDeliveries)
	}
	if ids, ok := result.failureBuckets["outbox row not found"]; !ok || len(ids) != 1 || ids[0] != 101 {
		t.Fatalf("unexpected failure buckets: %+v", result.failureBuckets)
	}
}

func TestDispatchDeliveryRows_PayloadValidationEjection(t *testing.T) {
	t.Parallel()

	sender := &testSender{failRoom: map[string]bool{}}
	d := newTestDispatcherForSend(t, sender)

	outboxByID := map[int64]domain.YouTubeNotificationOutbox{
		1: {ID: 1, ChannelID: "UCch1", Kind: domain.OutboxKindNewShort, Payload: `{"video_id":"s1","title":"ok"}`},
		2: {ID: 2, ChannelID: "UCch1", Kind: domain.OutboxKindNewShort, Payload: `{broken json`},
		3: {ID: 3, ChannelID: "UCch1", Kind: domain.OutboxKindNewShort, Payload: `{"video_id":"s3","title":"ok"}`},
	}

	rows := []domain.YouTubeNotificationDelivery{
		{ID: 101, OutboxID: 1, RoomID: "room1"},
		{ID: 102, OutboxID: 2, RoomID: "room1"},
		{ID: 103, OutboxID: 3, RoomID: "room1"},
	}

	result := d.dispatchDeliveryRows(context.Background(), rows, outboxByID)

	totalProcessed := len(result.successDeliveryIDs) + result.failedDeliveries
	if totalProcessed != 3 {
		t.Fatalf("total processed = %d, want 3", totalProcessed)
	}
	if result.failedDeliveries != 1 {
		t.Fatalf("failedDeliveries = %d, want 1 (broken payload)", result.failedDeliveries)
	}
	if len(result.successDeliveryIDs) != 2 {
		t.Fatalf("successDeliveryIDs count = %d, want 2", len(result.successDeliveryIDs))
	}
}

func TestDispatchDeliveryRows_MixedBatch(t *testing.T) {
	t.Parallel()

	sender := &testSender{failRoom: map[string]bool{}}
	d := newTestDispatcherForSend(t, sender)

	outboxByID := map[int64]domain.YouTubeNotificationOutbox{
		1: {ID: 1, ChannelID: "UCch1", Kind: domain.OutboxKindNewShort, Payload: `{"video_id":"s1","title":"쇼츠1"}`},
		2: {ID: 2, ChannelID: "UCch1", Kind: domain.OutboxKindNewShort, Payload: `{"video_id":"s2","title":"쇼츠2"}`},
		3: {ID: 3, ChannelID: "UCch1", Kind: domain.OutboxKindNewVideo, Payload: `{"video_id":"v1","title":"영상1"}`},
		4: {ID: 4, ChannelID: "UCch1", Kind: domain.OutboxKindMilestone, Payload: `{"milestone":"100만"}`},
	}

	rows := []domain.YouTubeNotificationDelivery{
		{ID: 101, OutboxID: 1, RoomID: "room1"},
		{ID: 102, OutboxID: 2, RoomID: "room1"},
		{ID: 103, OutboxID: 3, RoomID: "room1"},
		{ID: 104, OutboxID: 4, RoomID: "room1"},
	}

	result := d.dispatchDeliveryRows(context.Background(), rows, outboxByID)

	if len(result.successDeliveryIDs) != 4 {
		t.Fatalf("successDeliveryIDs = %d, want 4", len(result.successDeliveryIDs))
	}
	if len(result.touchedOutboxIDs) != 4 {
		t.Fatalf("touchedOutboxIDs = %d, want 4", len(result.touchedOutboxIDs))
	}
}

func TestDispatchDeliveryRows_SendFailure(t *testing.T) {
	t.Parallel()

	sender := &testSender{failRoom: map[string]bool{"room1": true}}
	d := newTestDispatcherForSend(t, sender)

	outboxByID := map[int64]domain.YouTubeNotificationOutbox{
		1: {ID: 1, ChannelID: "UCch1", Kind: domain.OutboxKindNewShort, Payload: `{"video_id":"s1","title":"쇼츠1"}`},
		2: {ID: 2, ChannelID: "UCch1", Kind: domain.OutboxKindNewShort, Payload: `{"video_id":"s2","title":"쇼츠2"}`},
	}

	rows := []domain.YouTubeNotificationDelivery{
		{ID: 101, OutboxID: 1, RoomID: "room1"},
		{ID: 102, OutboxID: 2, RoomID: "room1"},
	}

	result := d.dispatchDeliveryRows(context.Background(), rows, outboxByID)

	if result.failedDeliveries != 2 {
		t.Fatalf("failedDeliveries = %d, want 2", result.failedDeliveries)
	}
	if len(result.successDeliveryIDs) != 0 {
		t.Fatalf("successDeliveryIDs = %d, want 0", len(result.successDeliveryIDs))
	}
}
