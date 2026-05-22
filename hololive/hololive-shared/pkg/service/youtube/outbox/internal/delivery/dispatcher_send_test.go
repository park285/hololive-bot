package delivery

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"github.com/kapu/hololive-shared/pkg/domain"
	sharedalarmkeys "github.com/kapu/hololive-shared/pkg/service/alarm/keys"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
	"github.com/kapu/hololive-shared/pkg/service/template"
	"github.com/kapu/hololive-shared/pkg/service/youtube/logschema"
)

func newTestDispatcherForSend(t *testing.T, sender *testSender) *Dispatcher {
	t.Helper()

	cache := cachemocks.NewLenientClient()

	return NewDispatcher(nil, cache, sender, nil, slog.New(slog.NewTextHandler(io.Discard, nil)), Config{
		BatchSize:           10,
		LockTimeout:         time.Minute,
		PollInterval:        time.Second,
		MaxRetries:          3,
		RetryBackoff:        time.Minute,
		DeliveryParallelism: 2,
	})
}

func TestCollectRoomsByChannelUsesTypedSubscriberLookup(t *testing.T) {
	t.Parallel()

	lookedUpKeys := make([]string, 0, 2)
	var lookedUpKeysMu sync.Mutex
	cache := cachemocks.NewStrictClient()
	cache.SMembersFunc = func(_ context.Context, key string) ([]string, error) {
		lookedUpKeysMu.Lock()
		lookedUpKeys = append(lookedUpKeys, key)
		lookedUpKeysMu.Unlock()
		switch key {
		case sharedalarmkeys.BuildChannelSubscriberKey("UCtarget", domain.AlarmTypeShorts):
			return []string{"room-shorts"}, nil
		case sharedalarmkeys.BuildChannelSubscriberKey("UCtarget", domain.AlarmTypeCommunity):
			return []string{"room-community"}, nil
		default:
			return nil, nil
		}
	}

	dispatcher := NewDispatcher(nil, cache, &testSender{failRoom: map[string]bool{}}, nil, slog.New(slog.NewTextHandler(io.Discard, nil)), Config{})
	roomsByChannel := dispatcher.collectRoomsByChannel(context.Background(), []domain.YouTubeNotificationOutbox{
		{ChannelID: "UCtarget", Kind: domain.OutboxKindNewShort},
		{ChannelID: "UCtarget", Kind: domain.OutboxKindCommunityPost},
		{ChannelID: "UCtarget", Kind: domain.OutboxKindNewShort},
	})

	lookedUpKeysMu.Lock()
	recordedKeys := append([]string(nil), lookedUpKeys...)
	lookedUpKeysMu.Unlock()

	if len(recordedKeys) != 2 {
		t.Fatalf("lookup count = %d, want 2", len(recordedKeys))
	}
	if !sameStrings(recordedKeys, []string{
		sharedalarmkeys.BuildChannelSubscriberKey("UCtarget", domain.AlarmTypeShorts),
		sharedalarmkeys.BuildChannelSubscriberKey("UCtarget", domain.AlarmTypeCommunity),
	}) {
		t.Fatalf("lookedUpKeys = %#v", recordedKeys)
	}

	targets, ok := roomsByChannel["UCtarget"]
	if !ok {
		t.Fatalf("roomsByChannel missing UCtarget")
	}
	if len(targets[domain.AlarmTypeShorts]) != 1 || !targets[domain.AlarmTypeShorts]["room-shorts"] {
		t.Fatalf("shorts rooms = %#v", targets[domain.AlarmTypeShorts])
	}
	if len(targets[domain.AlarmTypeCommunity]) != 1 || !targets[domain.AlarmTypeCommunity]["room-community"] {
		t.Fatalf("community rooms = %#v", targets[domain.AlarmTypeCommunity])
	}
}

func TestCollectRoomsByChannelRespectsSubscriberLookupParallelism(t *testing.T) {
	t.Parallel()

	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	secondStarted := make(chan struct{}, 1)
	var callCount int32

	cache := cachemocks.NewStrictClient()
	cache.SMembersFunc = func(_ context.Context, key string) ([]string, error) {
		callNumber := atomic.AddInt32(&callCount, 1)
		if callNumber == 1 {
			close(firstStarted)
			<-releaseFirst
		} else {
			secondStarted <- struct{}{}
		}

		switch key {
		case sharedalarmkeys.BuildChannelSubscriberKey("UCtarget", domain.AlarmTypeShorts):
			return []string{"room-shorts"}, nil
		case sharedalarmkeys.BuildChannelSubscriberKey("UCtarget", domain.AlarmTypeCommunity):
			return []string{"room-community"}, nil
		default:
			return nil, nil
		}
	}

	dispatcher := NewDispatcher(nil, cache, &testSender{failRoom: map[string]bool{}}, nil, slog.New(slog.NewTextHandler(io.Discard, nil)), Config{
		SubscriberLookupParallelism: 1,
	})

	done := make(chan map[string]channelAlarmRoomTargets, 1)
	go func() {
		done <- dispatcher.collectRoomsByChannel(context.Background(), []domain.YouTubeNotificationOutbox{
			{ChannelID: "UCtarget", Kind: domain.OutboxKindNewShort},
			{ChannelID: "UCtarget", Kind: domain.OutboxKindCommunityPost},
		})
	}()

	select {
	case <-firstStarted:
	case <-time.After(time.Second):
		t.Fatal("first subscriber lookup did not start")
	}

	select {
	case <-secondStarted:
		t.Fatal("second subscriber lookup started before first finished")
	case <-time.After(20 * time.Millisecond):
	}

	close(releaseFirst)

	var roomsByChannel map[string]channelAlarmRoomTargets
	select {
	case roomsByChannel = <-done:
	case <-time.After(time.Second):
		t.Fatal("collectRoomsByChannel() did not complete")
	}

	if atomic.LoadInt32(&callCount) != 2 {
		t.Fatalf("lookup count = %d, want 2", atomic.LoadInt32(&callCount))
	}

	targets, ok := roomsByChannel["UCtarget"]
	if !ok {
		t.Fatalf("roomsByChannel missing UCtarget")
	}
	if len(targets[domain.AlarmTypeShorts]) != 1 || !targets[domain.AlarmTypeShorts]["room-shorts"] {
		t.Fatalf("shorts rooms = %#v", targets[domain.AlarmTypeShorts])
	}
	if len(targets[domain.AlarmTypeCommunity]) != 1 || !targets[domain.AlarmTypeCommunity]["room-community"] {
		t.Fatalf("community rooms = %#v", targets[domain.AlarmTypeCommunity])
	}
}

func sameStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}

	counts := make(map[string]int, len(left))
	for _, item := range left {
		counts[item]++
	}
	for _, item := range right {
		counts[item]--
		if counts[item] < 0 {
			return false
		}
	}
	for _, count := range counts {
		if count != 0 {
			return false
		}
	}
	return true
}

func TestGroupDeliveryRows(t *testing.T) {
	t.Parallel()

	outboxByID := map[int64]domain.YouTubeNotificationOutbox{
		1: {ID: 1, ChannelID: "UCch1", Kind: domain.OutboxKindNewShort, ContentID: "short-1", Payload: `{"video_id":"s1","title":"쇼츠1"}`},
		2: {ID: 2, ChannelID: "UCch1", Kind: domain.OutboxKindNewShort, ContentID: "short-2", Payload: `{"video_id":"s2","title":"쇼츠2"}`},
		3: {ID: 3, ChannelID: "UCch1", Kind: domain.OutboxKindNewVideo, ContentID: "video-1", Payload: `{"video_id":"v1","title":"영상1"}`},
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

func TestBuildDeliverySendRequest(t *testing.T) {
	t.Parallel()

	req, err := buildDeliverySendRequest("room1", "message", []domain.YouTubeNotificationOutbox{
		{Kind: domain.OutboxKindNewShort, ContentID: "short-1"},
		{Kind: domain.OutboxKindCommunityPost, ContentID: "post-1"},
	})
	if err != nil {
		t.Fatalf("buildDeliverySendRequest() error = %v", err)
	}
	if req.roomID != "room1" {
		t.Fatalf("roomID = %q, want room1", req.roomID)
	}
	if len(req.dedupeKeys) != 2 {
		t.Fatalf("dedupeKeys count = %d, want 2", len(req.dedupeKeys))
	}
	if req.dedupeKeys[0] != "youtube-notification:NEW_SHORT:short-1" {
		t.Fatalf("first dedupe key = %q", req.dedupeKeys[0])
	}
	if req.dedupeKeys[1] != "youtube-notification:COMMUNITY_POST:post-1" {
		t.Fatalf("second dedupe key = %q", req.dedupeKeys[1])
	}
}

func TestBuildDeliverySendRequestRejectsEmptyDedupeKey(t *testing.T) {
	t.Parallel()

	_, err := buildDeliverySendRequest("room1", "message", []domain.YouTubeNotificationOutbox{
		{Kind: domain.OutboxKindNewShort, ContentID: "   "},
	})
	if err == nil {
		t.Fatalf("buildDeliverySendRequest() error = nil, want error")
	}
	if !errors.Is(err, ErrDeliveryDedupeKeyRequired) {
		t.Fatalf("buildDeliverySendRequest() error = %v, want ErrDeliveryDedupeKeyRequired", err)
	}
}

func TestSendDeliveryMessageRejectsMissingDedupeKeysWithoutSending(t *testing.T) {
	t.Parallel()

	sender := &testSender{failRoom: map[string]bool{}}
	d := newTestDispatcherForSend(t, sender)

	err := d.sendDeliveryMessage(context.Background(), deliverySendRequest{roomID: "room1", message: "message"})
	if err == nil {
		t.Fatal("sendDeliveryMessage() error = nil, want error")
	}
	if !errors.Is(err, ErrDeliveryDedupeKeyRequired) {
		t.Fatalf("sendDeliveryMessage() error = %v, want ErrDeliveryDedupeKeyRequired", err)
	}

	sender.mu.Lock()
	defer sender.mu.Unlock()
	if len(sender.messages) != 0 {
		t.Fatalf("sender message count = %d, want 0", len(sender.messages))
	}
}

func TestSendDeliveryMessageRejectsBlankDedupeKeyWithoutSending(t *testing.T) {
	t.Parallel()

	sender := &testSender{failRoom: map[string]bool{}}
	d := newTestDispatcherForSend(t, sender)

	err := d.sendDeliveryMessage(context.Background(), deliverySendRequest{
		roomID:     "room1",
		message:    "message",
		dedupeKeys: []string{"youtube-notification:NEW_SHORT:short-1", "   "},
	})
	if err == nil {
		t.Fatal("sendDeliveryMessage() error = nil, want error")
	}
	if !errors.Is(err, ErrDeliveryDedupeKeyRequired) {
		t.Fatalf("sendDeliveryMessage() error = %v, want ErrDeliveryDedupeKeyRequired", err)
	}

	sender.mu.Lock()
	defer sender.mu.Unlock()
	if len(sender.messages) != 0 {
		t.Fatalf("sender message count = %d, want 0", len(sender.messages))
	}
}

func TestSendDeliveryMessagePassesStableClientRequestID(t *testing.T) {
	t.Parallel()

	sender := &testSender{failRoom: map[string]bool{}}
	dispatcher := newTestDispatcherForSend(t, sender)
	req := deliverySendRequest{
		roomID:     "room1",
		message:    "message",
		dedupeKeys: []string{"youtube-notification:NEW_SHORT:short-1"},
	}

	if err := dispatcher.sendDeliveryMessage(context.Background(), req); err != nil {
		t.Fatalf("sendDeliveryMessage() error = %v", err)
	}
	if err := dispatcher.sendDeliveryMessage(context.Background(), req); err != nil {
		t.Fatalf("sendDeliveryMessage() repeat error = %v", err)
	}
	otherRoomReq := req
	otherRoomReq.roomID = "room2"
	if err := dispatcher.sendDeliveryMessage(context.Background(), otherRoomReq); err != nil {
		t.Fatalf("sendDeliveryMessage() other room error = %v", err)
	}

	sender.mu.Lock()
	defer sender.mu.Unlock()
	if len(sender.clientRequestIDs) != 3 {
		t.Fatalf("clientRequestIDs count = %d, want 3", len(sender.clientRequestIDs))
	}
	if sender.clientRequestIDs[0] == "" || !strings.HasPrefix(sender.clientRequestIDs[0], "hololive-outbox:") {
		t.Fatalf("clientRequestID = %q, want hololive-outbox prefix", sender.clientRequestIDs[0])
	}
	if sender.clientRequestIDs[0] != sender.clientRequestIDs[1] {
		t.Fatalf("clientRequestID repeat = %q, want %q", sender.clientRequestIDs[1], sender.clientRequestIDs[0])
	}
	if sender.clientRequestIDs[2] == sender.clientRequestIDs[0] {
		t.Fatalf("clientRequestID for different room reused %q", sender.clientRequestIDs[2])
	}
}

func TestDispatchDeliveryRows_GroupedFallback(t *testing.T) {
	t.Parallel()

	sender := &testSender{failRoom: map[string]bool{}}
	d := newTestDispatcherForSend(t, sender)

	outboxByID := map[int64]domain.YouTubeNotificationOutbox{
		1: {ID: 1, ChannelID: "UCch1", Kind: domain.OutboxKindNewShort, ContentID: "short-1", Payload: `{"video_id":"s1","title":"쇼츠1"}`},
		2: {ID: 2, ChannelID: "UCch1", Kind: domain.OutboxKindNewShort, ContentID: "short-2", Payload: `{"video_id":"s2","title":"쇼츠2"}`},
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
		1: {ID: 1, ChannelID: "UCch1", Kind: domain.OutboxKindNewShort, ContentID: "short-1", Payload: `{"video_id":"s1","title":"ok"}`},
		2: {ID: 2, ChannelID: "UCch1", Kind: domain.OutboxKindNewShort, ContentID: "short-2", Payload: `{broken json`},
		3: {ID: 3, ChannelID: "UCch1", Kind: domain.OutboxKindNewShort, ContentID: "short-3", Payload: `{"video_id":"s3","title":"ok"}`},
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
		1: {ID: 1, ChannelID: "UCch1", Kind: domain.OutboxKindNewShort, ContentID: "short-1", Payload: `{"video_id":"s1","title":"쇼츠1"}`},
		2: {ID: 2, ChannelID: "UCch1", Kind: domain.OutboxKindNewShort, ContentID: "short-2", Payload: `{"video_id":"s2","title":"쇼츠2"}`},
		3: {ID: 3, ChannelID: "UCch1", Kind: domain.OutboxKindNewVideo, ContentID: "video-1", Payload: `{"video_id":"v1","title":"영상1"}`},
		4: {ID: 4, ChannelID: "UCch1", Kind: domain.OutboxKindMilestone, ContentID: "milestone-1", Payload: `{"milestone":"100만"}`},
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
		1: {ID: 1, ChannelID: "UCch1", Kind: domain.OutboxKindNewShort, ContentID: "short-1", Payload: `{"video_id":"s1","title":"쇼츠1"}`},
		2: {ID: 2, ChannelID: "UCch1", Kind: domain.OutboxKindNewShort, ContentID: "short-2", Payload: `{"video_id":"s2","title":"쇼츠2"}`},
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

func TestDispatchDeliveryRows_EmptyDedupeKeyFailsWithoutSending(t *testing.T) {
	t.Parallel()

	sender := &testSender{failRoom: map[string]bool{}}
	d := newTestDispatcherForSend(t, sender)

	outboxByID := map[int64]domain.YouTubeNotificationOutbox{
		1: {ID: 1, ChannelID: "UCch1", Kind: domain.OutboxKindNewShort, ContentID: "   ", Payload: `{"video_id":"s1","title":"쇼츠1"}`},
	}

	rows := []domain.YouTubeNotificationDelivery{
		{ID: 101, OutboxID: 1, RoomID: "room1"},
	}

	result := d.dispatchDeliveryRows(context.Background(), rows, outboxByID)

	if result.failedDeliveries != 1 {
		t.Fatalf("failedDeliveries = %d, want 1", result.failedDeliveries)
	}
	if ids, ok := result.failureBuckets["dedupe key"]; !ok || len(ids) != 1 || ids[0] != 101 {
		t.Fatalf("unexpected failure buckets: %+v", result.failureBuckets)
	}

	sender.mu.Lock()
	defer sender.mu.Unlock()
	if len(sender.messages) != 0 {
		t.Fatalf("sender message count = %d, want 0", len(sender.messages))
	}
}

func TestDispatchDeliveryRows_PerRoomSuccessLogsDedupeKey(t *testing.T) {
	t.Parallel()

	sender := &testSender{failRoom: map[string]bool{}}
	d, logBuffer := newLoggedTestDispatcherForSend(t, sender, nil)

	outboxByID := map[int64]domain.YouTubeNotificationOutbox{
		1: {ID: 1, ChannelID: "UCch1", Kind: domain.OutboxKindNewShort, ContentID: "short-1", Payload: `{"video_id":"s1","title":"쇼츠1"}`},
	}
	rows := []domain.YouTubeNotificationDelivery{{ID: 101, OutboxID: 1, RoomID: "room1"}}

	result := d.dispatchDeliveryRows(context.Background(), rows, outboxByID)

	if len(result.successDeliveryIDs) != 1 {
		t.Fatalf("successDeliveryIDs = %d, want 1", len(result.successDeliveryIDs))
	}

	entry := findLogEntryByMessage(t, logBuffer, "Sent per-room delivery")
	assertLogDedupeKeys(t, entry, []string{"youtube-notification:NEW_SHORT:short-1"})
}

func TestDispatchDeliveryRows_GroupedSuccessLogsDedupeKey(t *testing.T) {
	t.Parallel()

	renderer := newGroupedTemplateRenderer(t, domain.TemplateKeyOutboxShortsGroup, "{{range .Items}}{{.Title}} {{.URL}}\n{{end}}")
	sender := &testSender{failRoom: map[string]bool{}}
	d, logBuffer := newLoggedTestDispatcherForSend(t, sender, renderer)

	outboxByID := map[int64]domain.YouTubeNotificationOutbox{
		1: {ID: 1, ChannelID: "UCch1", Kind: domain.OutboxKindNewShort, ContentID: "short-1", Payload: `{"video_id":"s1","title":"쇼츠1"}`},
		2: {ID: 2, ChannelID: "UCch1", Kind: domain.OutboxKindNewShort, ContentID: "short-2", Payload: `{"video_id":"s2","title":"쇼츠2"}`},
	}
	rows := []domain.YouTubeNotificationDelivery{
		{ID: 101, OutboxID: 1, RoomID: "room1"},
		{ID: 102, OutboxID: 2, RoomID: "room1"},
	}

	result := d.dispatchDeliveryRows(context.Background(), rows, outboxByID)

	if len(result.successDeliveryIDs) != 2 {
		t.Fatalf("successDeliveryIDs = %d, want 2", len(result.successDeliveryIDs))
	}

	entry := findLogEntryByMessage(t, logBuffer, "Sent grouped delivery")
	assertLogDedupeKeys(t, entry, []string{
		"youtube-notification:NEW_SHORT:short-1",
		"youtube-notification:NEW_SHORT:short-2",
	})
}

func TestDispatchDeliveryRows_GroupedSendFailureLogsDedupeKey(t *testing.T) {
	t.Parallel()

	renderer := newGroupedTemplateRenderer(t, domain.TemplateKeyOutboxShortsGroup, "{{range .Items}}{{.Title}} {{.URL}}\n{{end}}")
	sender := &testSender{failRoom: map[string]bool{"room1": true}}
	d, logBuffer := newLoggedTestDispatcherForSend(t, sender, renderer)

	outboxByID := map[int64]domain.YouTubeNotificationOutbox{
		1: {ID: 1, ChannelID: "UCch1", Kind: domain.OutboxKindNewShort, ContentID: "short-1", Payload: `{"video_id":"s1","title":"쇼츠1"}`},
		2: {ID: 2, ChannelID: "UCch1", Kind: domain.OutboxKindNewShort, ContentID: "short-2", Payload: `{"video_id":"s2","title":"쇼츠2"}`},
	}
	rows := []domain.YouTubeNotificationDelivery{
		{ID: 101, OutboxID: 1, RoomID: "room1"},
		{ID: 102, OutboxID: 2, RoomID: "room1"},
	}

	result := d.dispatchDeliveryRows(context.Background(), rows, outboxByID)

	if result.failedDeliveries != 2 {
		t.Fatalf("failedDeliveries = %d, want 2", result.failedDeliveries)
	}

	entry := findLogEntryByMessage(t, logBuffer, "Failed to send grouped delivery")
	assertLogDedupeKeys(t, entry, []string{
		"youtube-notification:NEW_SHORT:short-1",
		"youtube-notification:NEW_SHORT:short-2",
	})
}

func TestDispatchDeliveryRows_PerRoomBuildFailureLogsDedupeKey(t *testing.T) {
	t.Parallel()

	sender := &testSender{failRoom: map[string]bool{}}
	d, logBuffer := newLoggedTestDispatcherForSend(t, sender, nil)

	outboxByID := map[int64]domain.YouTubeNotificationOutbox{
		1: {ID: 1, ChannelID: "UCch1", Kind: domain.OutboxKindNewShort, ContentID: "   ", Payload: `{"video_id":"s1","title":"쇼츠1"}`},
	}
	rows := []domain.YouTubeNotificationDelivery{{ID: 101, OutboxID: 1, RoomID: "room1"}}

	result := d.dispatchDeliveryRows(context.Background(), rows, outboxByID)

	if result.failedDeliveries != 1 {
		t.Fatalf("failedDeliveries = %d, want 1", result.failedDeliveries)
	}

	entry := findLogEntryByMessage(t, logBuffer, "Failed to build per-room delivery request")
	assertLogDedupeKeys(t, entry, []string{"invalid:NEW_SHORT:"})
}

func TestDispatchDeliveryRows_PerRoomSuccessLogsCommunityShortsAttemptStarted(t *testing.T) {
	t.Parallel()

	sender := &testSender{failRoom: map[string]bool{}}
	d, logBuffer := newLoggedTestDispatcherForSend(t, sender, nil)

	lockedAt := time.Date(2026, time.April, 10, 1, 0, 0, 0, time.UTC)
	outboxByID := map[int64]domain.YouTubeNotificationOutbox{
		1: {ID: 1, ChannelID: "UCch1", Kind: domain.OutboxKindNewShort, ContentID: "short-1", Payload: `{"video_id":"s1","title":"쇼츠1"}`},
	}
	rows := []domain.YouTubeNotificationDelivery{{ID: 101, OutboxID: 1, RoomID: "room1", AttemptCount: 2, LockedAt: &lockedAt}}

	result := d.dispatchDeliveryRows(context.Background(), rows, outboxByID)

	if len(result.successDeliveryIDs) != 1 {
		t.Fatalf("successDeliveryIDs = %d, want 1", len(result.successDeliveryIDs))
	}

	entry := findLogEntryByMessage(t, logBuffer, deliveryAttemptStartedLogMessage)
	assertSendLogStringField(t, entry, deliveryAuditContentIDLogField, "short-1")
	assertSendLogStringField(t, entry, deliveryAuditPostIDLogField, "short-1")
	assertSendLogStringField(t, entry, deliveryAuditAlarmTypeLogField, string(domain.AlarmTypeShorts))
	assertSendLogStringField(t, entry, deliveryAuditPathLogField, communityShortsDeliveryPath)
	assertSendLogStringField(t, entry, deliveryAuditModeLogField, "per_room")
	assertSendLogStringField(t, entry, deliveryDedupeKeyLogField, "youtube-notification:NEW_SHORT:short-1")
	assertSendLogStringField(t, entry, logschema.FieldRoomID, "room1")
	assertSendLogIntField(t, entry, logschema.FieldAttemptOrdinal, 3)

	startedAt := readSendLogTimeField(t, entry, deliveryAttemptStartedAtLogField)
	if !startedAt.After(lockedAt) {
		t.Fatalf("attempt_started_at = %s, want after locked_at %s", startedAt.Format(time.RFC3339Nano), lockedAt.Format(time.RFC3339Nano))
	}
}

func TestDispatchDeliveryRows_GroupedFailureLogsCommunityShortsAttemptStarted(t *testing.T) {
	t.Parallel()

	renderer := newGroupedTemplateRenderer(t, domain.TemplateKeyOutboxCommunityGroup, "{{range .Items}}{{.ContentText}} {{.URL}}\n{{end}}")
	sender := &testSender{failRoom: map[string]bool{"room1": true}}
	d, logBuffer := newLoggedTestDispatcherForSend(t, sender, renderer)

	lockedAt := time.Date(2026, time.April, 10, 2, 0, 0, 0, time.UTC)
	outboxByID := map[int64]domain.YouTubeNotificationOutbox{
		1: {ID: 1, ChannelID: "UCch1", Kind: domain.OutboxKindCommunityPost, ContentID: "post-1", Payload: `{"post_id":"post-1","content_text":"커뮤니티1"}`},
		2: {ID: 2, ChannelID: "UCch1", Kind: domain.OutboxKindCommunityPost, ContentID: "post-2", Payload: `{"post_id":"post-2","content_text":"커뮤니티2"}`},
	}
	rows := []domain.YouTubeNotificationDelivery{
		{ID: 101, OutboxID: 1, RoomID: "room1", AttemptCount: 1, LockedAt: &lockedAt},
		{ID: 102, OutboxID: 2, RoomID: "room1", AttemptCount: 1, LockedAt: &lockedAt},
	}

	result := d.dispatchDeliveryRows(context.Background(), rows, outboxByID)

	if result.failedDeliveries != 2 {
		t.Fatalf("failedDeliveries = %d, want 2", result.failedDeliveries)
	}

	entries := findAllSendLogEntriesByMessage(t, logBuffer, deliveryAttemptStartedLogMessage)
	if len(entries) != 2 {
		t.Fatalf("attempt start entry count = %d, want 2", len(entries))
	}

	contentIDs := make([]string, 0, len(entries))
	postIDs := make([]string, 0, len(entries))
	dedupeKeys := make([]string, 0, len(entries))
	for i := range entries {
		assertSendLogStringField(t, entries[i], deliveryAuditAlarmTypeLogField, string(domain.AlarmTypeCommunity))
		assertSendLogStringField(t, entries[i], deliveryAuditPathLogField, communityShortsDeliveryPath)
		assertSendLogStringField(t, entries[i], deliveryAuditModeLogField, "grouped")
		assertSendLogStringField(t, entries[i], logschema.FieldRoomID, "room1")
		assertSendLogIntField(t, entries[i], logschema.FieldAttemptOrdinal, 2)

		startedAt := readSendLogTimeField(t, entries[i], deliveryAttemptStartedAtLogField)
		if !startedAt.After(lockedAt) {
			t.Fatalf("attempt_started_at = %s, want after locked_at %s", startedAt.Format(time.RFC3339Nano), lockedAt.Format(time.RFC3339Nano))
		}

		contentIDs = append(contentIDs, readSendLogStringField(t, entries[i], deliveryAuditContentIDLogField))
		postIDs = append(postIDs, readSendLogStringField(t, entries[i], deliveryAuditPostIDLogField))
		dedupeKeys = append(dedupeKeys, readSendLogStringField(t, entries[i], deliveryDedupeKeyLogField))
	}

	if !sameStrings(contentIDs, []string{"post-1", "post-2"}) {
		t.Fatalf("attempt start content IDs = %#v", contentIDs)
	}
	if !sameStrings(postIDs, []string{"post-1", "post-2"}) {
		t.Fatalf("attempt start post IDs = %#v", postIDs)
	}
	if !sameStrings(dedupeKeys, []string{
		"youtube-notification:COMMUNITY_POST:post-1",
		"youtube-notification:COMMUNITY_POST:post-2",
	}) {
		t.Fatalf("attempt start dedupe keys = %#v", dedupeKeys)
	}
}

func TestDispatchDeliveryRows_VideoDoesNotLogCommunityShortsAttemptStarted(t *testing.T) {
	t.Parallel()

	sender := &testSender{failRoom: map[string]bool{}}
	d, logBuffer := newLoggedTestDispatcherForSend(t, sender, nil)

	outboxByID := map[int64]domain.YouTubeNotificationOutbox{
		1: {ID: 1, ChannelID: "UCch1", Kind: domain.OutboxKindNewVideo, ContentID: "video-1", Payload: `{"video_id":"v1","title":"영상1"}`},
	}
	rows := []domain.YouTubeNotificationDelivery{{ID: 101, OutboxID: 1, RoomID: "room1"}}

	result := d.dispatchDeliveryRows(context.Background(), rows, outboxByID)

	if len(result.successDeliveryIDs) != 1 {
		t.Fatalf("successDeliveryIDs = %d, want 1", len(result.successDeliveryIDs))
	}

	entries := findAllSendLogEntriesByMessage(t, logBuffer, deliveryAttemptStartedLogMessage)
	if len(entries) != 0 {
		t.Fatalf("attempt start entry count = %d, want 0", len(entries))
	}
}

func TestDispatchDeliveryRows_GroupedSuccessLogsCommunityShortsResult(t *testing.T) {
	t.Parallel()

	renderer := newGroupedTemplateRenderer(t, domain.TemplateKeyOutboxShortsGroup, "{{range .Items}}{{.Title}} {{.URL}}\n{{end}}")
	sender := &testSender{failRoom: map[string]bool{}}
	d, logBuffer := newLoggedTestDispatcherForSend(t, sender, renderer)

	outboxByID := map[int64]domain.YouTubeNotificationOutbox{
		1: {ID: 1, ChannelID: "UCch1", Kind: domain.OutboxKindNewShort, ContentID: "short-1", Payload: `{"video_id":"s1","title":"쇼츠1"}`},
		2: {ID: 2, ChannelID: "UCch1", Kind: domain.OutboxKindNewShort, ContentID: "short-2", Payload: `{"video_id":"s2","title":"쇼츠2"}`},
	}
	rows := []domain.YouTubeNotificationDelivery{
		{ID: 101, OutboxID: 1, RoomID: "room1"},
		{ID: 102, OutboxID: 2, RoomID: "room1"},
	}

	result := d.dispatchDeliveryRows(context.Background(), rows, outboxByID)

	if len(result.successDeliveryIDs) != 2 {
		t.Fatalf("successDeliveryIDs = %d, want 2", len(result.successDeliveryIDs))
	}

	entry := findLogEntryByMessage(t, logBuffer, deliveryResultLogMessage)
	assertSendLogStringField(t, entry, logschema.FieldChannelID, "UCch1")
	assertSendLogStringField(t, entry, deliveryAuditAlarmTypeLogField, string(domain.AlarmTypeShorts))
	assertSendLogStringField(t, entry, deliveryAuditSendResultLogField, "success")
	assertSendLogStringField(t, entry, deliveryAuditPathLogField, communityShortsDeliveryPath)
	assertSendLogStringField(t, entry, deliveryAuditModeLogField, "grouped")
	assertSendLogStringField(t, entry, logschema.FieldRoomID, "room1")
	assertSendLogIntField(t, entry, logschema.FieldTargetAlarmCount, 2)
	assertSendLogIntField(t, entry, logschema.FieldSuccessfulAlarmCount, 2)
	assertSendLogIntField(t, entry, logschema.FieldFailedAlarmCount, 0)
	assertSendLogIntField(t, entry, logschema.FieldTargetRoomCount, 1)
	assertSendLogIntField(t, entry, logschema.FieldSuccessfulRoomCount, 1)
	assertSendLogIntField(t, entry, logschema.FieldFailedRoomCount, 0)
	assertSendLogTimeField(t, entry, deliveryAuditSentAtLogField)
}

func TestDispatchDeliveryRows_PerRoomFailureLogsCommunityShortsResult(t *testing.T) {
	t.Parallel()

	sender := &testSender{failRoom: map[string]bool{"room1": true}}
	d, logBuffer := newLoggedTestDispatcherForSend(t, sender, nil)

	outboxByID := map[int64]domain.YouTubeNotificationOutbox{
		1: {ID: 1, ChannelID: "UCch1", Kind: domain.OutboxKindCommunityPost, ContentID: "post-1", Payload: `{"post_id":"post-1","content_text":"커뮤니티1"}`},
	}
	rows := []domain.YouTubeNotificationDelivery{{ID: 101, OutboxID: 1, RoomID: "room1"}}

	result := d.dispatchDeliveryRows(context.Background(), rows, outboxByID)

	if result.failedDeliveries != 1 {
		t.Fatalf("failedDeliveries = %d, want 1", result.failedDeliveries)
	}

	entry := findLogEntryByMessage(t, logBuffer, deliveryResultLogMessage)
	assertSendLogStringField(t, entry, logschema.FieldChannelID, "UCch1")
	assertSendLogStringField(t, entry, deliveryAuditAlarmTypeLogField, string(domain.AlarmTypeCommunity))
	assertSendLogStringField(t, entry, deliveryAuditSendResultLogField, "failure")
	assertSendLogStringField(t, entry, deliveryAuditFailureReasonLogField, "send message")
	assertSendLogStringField(t, entry, deliveryAuditPathLogField, communityShortsDeliveryPath)
	assertSendLogStringField(t, entry, deliveryAuditModeLogField, "per_room")
	assertSendLogStringField(t, entry, logschema.FieldRoomID, "room1")
	assertSendLogIntField(t, entry, logschema.FieldTargetAlarmCount, 1)
	assertSendLogIntField(t, entry, logschema.FieldSuccessfulAlarmCount, 0)
	assertSendLogIntField(t, entry, logschema.FieldFailedAlarmCount, 1)
	assertSendLogIntField(t, entry, logschema.FieldTargetRoomCount, 1)
	assertSendLogIntField(t, entry, logschema.FieldSuccessfulRoomCount, 0)
	assertSendLogIntField(t, entry, logschema.FieldFailedRoomCount, 1)
	assertSendLogTimeField(t, entry, deliveryAuditSentAtLogField)
}

func TestDispatchDeliveryRows_VideoDoesNotLogCommunityShortsResult(t *testing.T) {
	t.Parallel()

	sender := &testSender{failRoom: map[string]bool{}}
	d, logBuffer := newLoggedTestDispatcherForSend(t, sender, nil)

	outboxByID := map[int64]domain.YouTubeNotificationOutbox{
		1: {ID: 1, ChannelID: "UCch1", Kind: domain.OutboxKindNewVideo, ContentID: "video-1", Payload: `{"video_id":"v1","title":"영상1"}`},
	}
	rows := []domain.YouTubeNotificationDelivery{{ID: 101, OutboxID: 1, RoomID: "room1"}}

	result := d.dispatchDeliveryRows(context.Background(), rows, outboxByID)

	if len(result.successDeliveryIDs) != 1 {
		t.Fatalf("successDeliveryIDs = %d, want 1", len(result.successDeliveryIDs))
	}

	entries := findAllSendLogEntriesByMessage(t, logBuffer, deliveryResultLogMessage)
	if len(entries) != 0 {
		t.Fatalf("delivery result entry count = %d, want 0", len(entries))
	}
}

func TestDispatchDeliveryRows_PerRoomSuccessLogsCommunityShortsAudit(t *testing.T) {
	t.Parallel()

	sender := &testSender{failRoom: map[string]bool{}}
	d, logBuffer := newLoggedTestDispatcherForSend(t, sender, nil)

	outboxByID := map[int64]domain.YouTubeNotificationOutbox{
		1: {ID: 1, ChannelID: "UCch1", Kind: domain.OutboxKindNewShort, ContentID: "short-1", Payload: `{"video_id":"s1","title":"쇼츠1"}`},
	}
	rows := []domain.YouTubeNotificationDelivery{{ID: 101, OutboxID: 1, RoomID: "room1"}}

	result := d.dispatchDeliveryRows(context.Background(), rows, outboxByID)

	if len(result.successDeliveryIDs) != 1 {
		t.Fatalf("successDeliveryIDs = %d, want 1", len(result.successDeliveryIDs))
	}

	entries := findAllSendLogEntriesByMessage(t, logBuffer, deliveryAuditLogMessage)
	if len(entries) != 1 {
		t.Fatalf("audit entry count = %d, want 1", len(entries))
	}

	assertSendLogStringField(t, entries[0], deliveryAuditContentIDLogField, "short-1")
	assertSendLogStringField(t, entries[0], deliveryAuditPostIDLogField, "short-1")
	assertSendLogStringField(t, entries[0], deliveryAuditAlarmTypeLogField, string(domain.AlarmTypeShorts))
	assertSendLogStringField(t, entries[0], deliveryAuditSendResultLogField, "success")
	assertSendLogStringField(t, entries[0], deliveryAuditPathLogField, communityShortsDeliveryPath)
	assertSendLogStringField(t, entries[0], deliveryAuditModeLogField, "per_room")
	assertSendLogStringField(t, entries[0], deliveryDedupeKeyLogField, "youtube-notification:NEW_SHORT:short-1")
	assertSendLogStringField(t, entries[0], logschema.FieldRoomID, "room1")
	assertSendLogTimeField(t, entries[0], deliveryAuditSentAtLogField)
}

func TestDispatchDeliveryRows_AuditUsesDetectionPostIDFieldSchema(t *testing.T) {
	t.Parallel()

	sender := &testSender{failRoom: map[string]bool{}}
	d, logBuffer := newLoggedTestDispatcherForSend(t, sender, nil)

	outboxByID := map[int64]domain.YouTubeNotificationOutbox{
		1: {
			ID:        1,
			ChannelID: "UCch1",
			Kind:      domain.OutboxKindCommunityPost,
			ContentID: "post-canonical",
			Payload:   `{"canonical_post_id":"post-canonical","post_id":"post-resource","content_text":"커뮤니티"}`,
		},
	}
	rows := []domain.YouTubeNotificationDelivery{{ID: 101, OutboxID: 1, RoomID: "room1"}}

	result := d.dispatchDeliveryRows(context.Background(), rows, outboxByID)

	if len(result.successDeliveryIDs) != 1 {
		t.Fatalf("successDeliveryIDs = %d, want 1", len(result.successDeliveryIDs))
	}

	entry := findLogEntryByMessage(t, logBuffer, deliveryAuditLogMessage)
	if _, exists := entry[logschema.FieldPostID]; !exists {
		t.Fatalf("audit log missing %q field: %#v", logschema.FieldPostID, entry)
	}
	if got := entry[logschema.FieldPostID]; got != "post-canonical" {
		t.Fatalf("audit post_id = %#v, want %q", got, "post-canonical")
	}
}

func TestDispatchDeliveryRows_GroupedFailureLogsCommunityShortsAudit(t *testing.T) {
	t.Parallel()

	renderer := newGroupedTemplateRenderer(t, domain.TemplateKeyOutboxCommunityGroup, "{{range .Items}}{{.ContentText}} {{.URL}}\n{{end}}")
	sender := &testSender{failRoom: map[string]bool{"room1": true}}
	d, logBuffer := newLoggedTestDispatcherForSend(t, sender, renderer)

	outboxByID := map[int64]domain.YouTubeNotificationOutbox{
		1: {ID: 1, ChannelID: "UCch1", Kind: domain.OutboxKindCommunityPost, ContentID: "post-1", Payload: `{"post_id":"post-1","content_text":"커뮤니티1"}`},
		2: {ID: 2, ChannelID: "UCch1", Kind: domain.OutboxKindCommunityPost, ContentID: "post-2", Payload: `{"post_id":"post-2","content_text":"커뮤니티2"}`},
	}
	rows := []domain.YouTubeNotificationDelivery{
		{ID: 101, OutboxID: 1, RoomID: "room1"},
		{ID: 102, OutboxID: 2, RoomID: "room1"},
	}

	result := d.dispatchDeliveryRows(context.Background(), rows, outboxByID)

	if result.failedDeliveries != 2 {
		t.Fatalf("failedDeliveries = %d, want 2", result.failedDeliveries)
	}

	entries := findAllSendLogEntriesByMessage(t, logBuffer, deliveryAuditLogMessage)
	if len(entries) != 2 {
		t.Fatalf("audit entry count = %d, want 2", len(entries))
	}

	contentIDs := make([]string, 0, len(entries))
	postIDs := make([]string, 0, len(entries))
	for i := range entries {
		assertSendLogStringField(t, entries[i], deliveryAuditAlarmTypeLogField, string(domain.AlarmTypeCommunity))
		assertSendLogStringField(t, entries[i], deliveryAuditSendResultLogField, "failure")
		assertSendLogStringField(t, entries[i], deliveryAuditFailureReasonLogField, "send message")
		assertSendLogStringField(t, entries[i], deliveryAuditPathLogField, communityShortsDeliveryPath)
		assertSendLogStringField(t, entries[i], deliveryAuditModeLogField, "grouped")
		assertSendLogTimeField(t, entries[i], deliveryAuditSentAtLogField)

		contentID := readSendLogStringField(t, entries[i], deliveryAuditContentIDLogField)
		contentIDs = append(contentIDs, contentID)
		postIDs = append(postIDs, readSendLogStringField(t, entries[i], deliveryAuditPostIDLogField))
	}

	if !sameStrings(contentIDs, []string{"post-1", "post-2"}) {
		t.Fatalf("audit content IDs = %#v", contentIDs)
	}
	if !sameStrings(postIDs, []string{"post-1", "post-2"}) {
		t.Fatalf("audit post IDs = %#v", postIDs)
	}
}

func TestDispatchDeliveryRows_VideoDoesNotLogCommunityShortsAudit(t *testing.T) {
	t.Parallel()

	sender := &testSender{failRoom: map[string]bool{}}
	d, logBuffer := newLoggedTestDispatcherForSend(t, sender, nil)

	outboxByID := map[int64]domain.YouTubeNotificationOutbox{
		1: {ID: 1, ChannelID: "UCch1", Kind: domain.OutboxKindNewVideo, ContentID: "video-1", Payload: `{"video_id":"v1","title":"영상1"}`},
	}
	rows := []domain.YouTubeNotificationDelivery{{ID: 101, OutboxID: 1, RoomID: "room1"}}

	result := d.dispatchDeliveryRows(context.Background(), rows, outboxByID)

	if len(result.successDeliveryIDs) != 1 {
		t.Fatalf("successDeliveryIDs = %d, want 1", len(result.successDeliveryIDs))
	}

	entries := findAllSendLogEntriesByMessage(t, logBuffer, deliveryAuditLogMessage)
	if len(entries) != 0 {
		t.Fatalf("audit entry count = %d, want 0", len(entries))
	}
}

type safeBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *safeBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *safeBuffer) Bytes() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	cloned := make([]byte, b.buf.Len())
	copy(cloned, b.buf.Bytes())
	return cloned
}

func (b *safeBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func newLoggedTestDispatcherForSend(t *testing.T, sender *testSender, renderer *template.Renderer) (*Dispatcher, *safeBuffer) {
	t.Helper()

	cache := cachemocks.NewLenientClient()
	logBuffer := &safeBuffer{}
	logger := slog.New(slog.NewJSONHandler(logBuffer, &slog.HandlerOptions{Level: slog.LevelDebug}))

	return NewDispatcher(nil, cache, sender, renderer, logger, Config{
		BatchSize:           10,
		LockTimeout:         time.Minute,
		PollInterval:        time.Second,
		MaxRetries:          3,
		RetryBackoff:        time.Minute,
		DeliveryParallelism: 2,
	}), logBuffer
}

func newGroupedTemplateRenderer(t *testing.T, key domain.TemplateKey, body string) *template.Renderer {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&domain.NotificationTemplate{},
		&domain.YouTubeCommunityShortsAlarmState{},
	); err != nil {
		t.Fatalf("migrate notification templates: %v", err)
	}
	if err := db.Create(&domain.NotificationTemplate{TemplateKey: key, Body: body}).Error; err != nil {
		t.Fatalf("seed grouped template: %v", err)
	}

	return template.NewRenderer(db, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func findLogEntryByMessage(t *testing.T, logBuffer *safeBuffer, message string) map[string]any {
	t.Helper()

	for line := range bytes.SplitSeq(bytes.TrimSpace(logBuffer.Bytes()), []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		entry := make(map[string]any)
		if err := json.Unmarshal(line, &entry); err != nil {
			t.Fatalf("unmarshal log entry: %v", err)
		}
		if entry["msg"] == message {
			return entry
		}
	}

	t.Fatalf("log message %q not found in %s", message, logBuffer.String())
	return nil
}

func findAllSendLogEntriesByMessage(t *testing.T, logBuffer *safeBuffer, message string) []map[string]any {
	t.Helper()

	entries := make([]map[string]any, 0)
	for line := range bytes.SplitSeq(bytes.TrimSpace(logBuffer.Bytes()), []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		entry := make(map[string]any)
		if err := json.Unmarshal(line, &entry); err != nil {
			t.Fatalf("unmarshal log entry: %v", err)
		}
		if entry["msg"] == message {
			entries = append(entries, entry)
		}
	}

	return entries
}

func readSendLogStringField(t *testing.T, entry map[string]any, field string) string {
	t.Helper()

	raw, ok := entry[field]
	if !ok {
		t.Fatalf("log entry missing %q: %#v", field, entry)
	}
	value, ok := raw.(string)
	if !ok {
		t.Fatalf("log field %q type = %T, want string", field, raw)
	}
	return value
}

func readSendLogTimeField(t *testing.T, entry map[string]any, field string) time.Time {
	t.Helper()

	value := readSendLogStringField(t, entry, field)
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		t.Fatalf("log field %q = %q, want RFC3339Nano time: %v", field, value, err)
	}
	return parsed.UTC()
}

func readSendLogIntField(t *testing.T, entry map[string]any, field string) int {
	t.Helper()

	raw, ok := entry[field]
	if !ok {
		t.Fatalf("log entry missing %q: %#v", field, entry)
	}
	switch value := raw.(type) {
	case float64:
		return int(value)
	case int:
		return value
	default:
		t.Fatalf("log field %q type = %T, want number", field, raw)
	}
	return 0
}

func assertSendLogStringField(t *testing.T, entry map[string]any, field, want string) {
	t.Helper()

	if got := readSendLogStringField(t, entry, field); got != want {
		t.Fatalf("log field %q = %q, want %q", field, got, want)
	}
}

func assertSendLogTimeField(t *testing.T, entry map[string]any, field string) {
	t.Helper()

	_ = readSendLogTimeField(t, entry, field)
}

func assertSendLogIntField(t *testing.T, entry map[string]any, field string, want int) {
	t.Helper()

	if got := readSendLogIntField(t, entry, field); got != want {
		t.Fatalf("log field %q = %d, want %d", field, got, want)
	}
}

func assertLogDedupeKeys(t *testing.T, entry map[string]any, want []string) {
	t.Helper()

	raw, ok := entry[deliveryDedupeKeyLogField]
	if !ok {
		t.Fatalf("log entry missing %q: %#v", deliveryDedupeKeyLogField, entry)
	}
	values, ok := raw.([]any)
	if !ok {
		t.Fatalf("dedupe_key type = %T, want []any", raw)
	}

	got := make([]string, 0, len(values))
	for i := range values {
		value, ok := values[i].(string)
		if !ok {
			t.Fatalf("dedupe_key[%d] type = %T, want string", i, values[i])
		}
		got = append(got, value)
	}

	if !sameStrings(got, want) {
		t.Fatalf("dedupe_key = %#v, want %#v", got, want)
	}
}

type blockingSender struct{}

func (s *blockingSender) SendMessage(ctx context.Context, _, _ string) error {
	<-ctx.Done()
	return ctx.Err()
}

type parentDeadlineBeforeReturnSender struct {
	parentCtx             context.Context
	parentDoneBeforeChild atomic.Bool
}

func (s *parentDeadlineBeforeReturnSender) SendMessage(ctx context.Context, _, _ string) error {
	<-ctx.Done()
	select {
	case <-s.parentCtx.Done():
		s.parentDoneBeforeChild.Store(true)
	default:
	}
	<-s.parentCtx.Done()
	return ctx.Err()
}

func TestSendDeliveryMessageUsesConfiguredTimeout(t *testing.T) {
	t.Parallel()

	dispatcher := NewDispatcher(nil,
		cachemocks.NewLenientClient(),
		&blockingSender{},
		nil,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		Config{DeliverySendTimeout: 5 * time.Millisecond},
	)

	err := dispatcher.sendDeliveryMessage(context.Background(), deliverySendRequest{
		roomID:     "room-timeout",
		message:    "hello",
		dedupeKeys: []string{"youtube-notification:NEW_SHORT:short-timeout"},
	})

	if err == nil {
		t.Fatal("sendDeliveryMessage() error = nil, want timeout")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("sendDeliveryMessage() error = %v, want context deadline exceeded", err)
	}
	if !strings.Contains(err.Error(), "timed out after 5ms") {
		t.Fatalf("sendDeliveryMessage() error = %q, want configured-timeout-specific message", err)
	}
}

func TestSendDeliveryMessageUsesParentDeadlineErrorPath(t *testing.T) {
	t.Parallel()

	dispatcher := NewDispatcher(nil,
		cachemocks.NewLenientClient(),
		&blockingSender{},
		nil,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		Config{DeliverySendTimeout: 100 * time.Millisecond},
	)

	parentCtx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()

	err := dispatcher.sendDeliveryMessage(parentCtx, deliverySendRequest{
		roomID:     "room-parent-timeout",
		message:    "hello",
		dedupeKeys: []string{"youtube-notification:NEW_SHORT:short-parent-timeout"},
	})

	if err == nil {
		t.Fatal("sendDeliveryMessage() error = nil, want parent deadline error")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("sendDeliveryMessage() error = %v, want context deadline exceeded", err)
	}
	if strings.Contains(err.Error(), "timed out after 100ms") {
		t.Fatalf("sendDeliveryMessage() error = %q, want parent deadline path without child timeout message", err)
	}
}

func TestSendDeliveryMessageUsesConfiguredTimeoutWhenParentExpiresBeforeReturn(t *testing.T) {
	t.Parallel()

	parentCtx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	sender := &parentDeadlineBeforeReturnSender{parentCtx: parentCtx}

	dispatcher := NewDispatcher(nil,
		cachemocks.NewLenientClient(),
		sender,
		nil,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		Config{DeliverySendTimeout: 5 * time.Millisecond},
	)

	err := dispatcher.sendDeliveryMessage(parentCtx, deliverySendRequest{
		roomID:     "room-child-timeout-first",
		message:    "hello",
		dedupeKeys: []string{"youtube-notification:NEW_SHORT:short-child-timeout-first"},
	})

	if err == nil {
		t.Fatal("sendDeliveryMessage() error = nil, want configured timeout")
	}
	if sender.parentDoneBeforeChild.Load() {
		t.Fatal("parent deadline expired before configured delivery timeout")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("sendDeliveryMessage() error = %v, want context deadline exceeded", err)
	}
	if !strings.Contains(err.Error(), "timed out after 5ms") {
		t.Fatalf("sendDeliveryMessage() error = %q, want configured-timeout-specific message", err)
	}
}

func TestNewDispatcherAppliesDeliveryDefaults(t *testing.T) {
	t.Parallel()

	dispatcher := NewDispatcher(nil,
		cachemocks.NewLenientClient(),
		&testSender{failRoom: map[string]bool{}},
		nil,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		Config{},
	)

	defaults := DefaultConfig()
	if dispatcher.cfg.DeliveryParallelism != defaults.DeliveryParallelism {
		t.Fatalf("DeliveryParallelism = %d, want %d", dispatcher.cfg.DeliveryParallelism, defaults.DeliveryParallelism)
	}
	if dispatcher.cfg.DeliverySendTimeout != defaults.DeliverySendTimeout {
		t.Fatalf("DeliverySendTimeout = %s, want %s", dispatcher.cfg.DeliverySendTimeout, defaults.DeliverySendTimeout)
	}
	if dispatcher.cfg.SubscriberLookupParallelism != defaults.SubscriberLookupParallelism {
		t.Fatalf("SubscriberLookupParallelism = %d, want %d", dispatcher.cfg.SubscriberLookupParallelism, defaults.SubscriberLookupParallelism)
	}
}
