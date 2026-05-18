package delivery

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
)

type youtubeOutboxKaringTestSender struct {
	mu       sync.Mutex
	messages []string
	payloads []domain.YouTubeOutboxDispatchPayload
	failErr  error
}

func (s *youtubeOutboxKaringTestSender) SendMessage(_ context.Context, roomID, message string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.messages = append(s.messages, roomID+":"+message)
	return nil
}

func (s *youtubeOutboxKaringTestSender) SendYouTubeOutboxKaring(_ context.Context, _ string, payload domain.YouTubeOutboxDispatchPayload) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failErr != nil {
		return s.failErr
	}
	s.payloads = append(s.payloads, payload)
	return nil
}

func TestDispatcherUsesKaringForSupportedYouTubeOutboxKind(t *testing.T) {
	t.Parallel()

	sender := &youtubeOutboxKaringTestSender{}
	dispatcher := NewDispatcher(nil, cachemocks.NewLenientClient(), sender, nil, slog.New(slog.NewTextHandler(io.Discard, nil)), Config{
		DeliveryParallelism: 1,
		DeliverySendTimeout: time.Second,
	})
	rows := []domain.YouTubeNotificationDelivery{
		{ID: 11, OutboxID: 101, RoomID: "room-1"},
		{ID: 12, OutboxID: 102, RoomID: "room-1"},
	}
	outboxByID := map[int64]domain.YouTubeNotificationOutbox{
		101: {ID: 101, ChannelID: "UCshorts", Kind: domain.OutboxKindNewShort, ContentID: "shorts:a", Payload: `{"video_id":"a","title":"short a"}`},
		102: {ID: 102, ChannelID: "UCshorts", Kind: domain.OutboxKindNewShort, ContentID: "shorts:b", Payload: `{"video_id":"b","title":"short b"}`},
	}

	result := dispatcher.dispatchDeliveryRows(context.Background(), rows, outboxByID)

	if len(result.successDeliveryIDs) != 2 {
		t.Fatalf("successDeliveryIDs = %#v, want 2 ids", result.successDeliveryIDs)
	}
	if len(sender.messages) != 0 {
		t.Fatalf("text messages = %#v, want none", sender.messages)
	}
	if len(sender.payloads) != 1 {
		t.Fatalf("karing payload count = %d, want 1", len(sender.payloads))
	}
	payload := sender.payloads[0]
	if payload.Kind != domain.OutboxKindNewShort || payload.AlarmType != domain.AlarmTypeShorts {
		t.Fatalf("payload kind/alarm_type = %s/%s", payload.Kind, payload.AlarmType)
	}
	if len(payload.Items) != 2 {
		t.Fatalf("payload items = %d, want 2", len(payload.Items))
	}
	if payload.Items[0].OutboxID != 101 || payload.Items[1].OutboxID != 102 {
		t.Fatalf("payload item outbox ids = %#v", payload.Items)
	}
}

func TestDispatcherFallsBackToTextForUnsupportedKaringKind(t *testing.T) {
	t.Parallel()

	sender := &youtubeOutboxKaringTestSender{}
	dispatcher := NewDispatcher(nil, cachemocks.NewLenientClient(), sender, nil, slog.New(slog.NewTextHandler(io.Discard, nil)), Config{
		DeliveryParallelism: 1,
		DeliverySendTimeout: time.Second,
	})
	rows := []domain.YouTubeNotificationDelivery{{ID: 21, OutboxID: 201, RoomID: "room-1"}}
	outboxByID := map[int64]domain.YouTubeNotificationOutbox{
		201: {ID: 201, ChannelID: "UCmilestone", Kind: domain.OutboxKindMilestone, ContentID: "milestone:1", Payload: `{"milestone":"100만"}`},
	}

	result := dispatcher.dispatchDeliveryRows(context.Background(), rows, outboxByID)

	if len(result.successDeliveryIDs) != 1 {
		t.Fatalf("successDeliveryIDs = %#v, want one id", result.successDeliveryIDs)
	}
	if len(sender.payloads) != 0 {
		t.Fatalf("karing payload count = %d, want 0", len(sender.payloads))
	}
	if len(sender.messages) != 1 {
		t.Fatalf("text messages = %#v, want one message", sender.messages)
	}
}

func TestDispatcherKaringFailureDoesNotFallBackToDuplicateText(t *testing.T) {
	t.Parallel()

	sender := &youtubeOutboxKaringTestSender{failErr: errors.New("karing failed")}
	dispatcher := NewDispatcher(nil, cachemocks.NewLenientClient(), sender, nil, slog.New(slog.NewTextHandler(io.Discard, nil)), Config{
		DeliveryParallelism: 1,
		DeliverySendTimeout: time.Second,
	})
	rows := []domain.YouTubeNotificationDelivery{{ID: 31, OutboxID: 301, RoomID: "room-1"}}
	outboxByID := map[int64]domain.YouTubeNotificationOutbox{
		301: {ID: 301, ChannelID: "UCcommunity", Kind: domain.OutboxKindCommunityPost, ContentID: "post:1", Payload: `{"post_id":"1","content_text":"hello"}`},
	}

	result := dispatcher.dispatchDeliveryRows(context.Background(), rows, outboxByID)

	if len(result.successDeliveryIDs) != 0 {
		t.Fatalf("successDeliveryIDs = %#v, want none", result.successDeliveryIDs)
	}
	if got := result.failureBuckets["karing send"]; len(got) != 1 || got[0] != 31 {
		t.Fatalf("karing send failure bucket = %#v, want [31]", got)
	}
	if len(sender.messages) != 0 {
		t.Fatalf("text messages = %#v, want none", sender.messages)
	}
}

func TestDispatcherSerializesKaringSends(t *testing.T) {
	sender := newBlockingKaringSender()
	dispatcher := NewDispatcher(nil, cachemocks.NewLenientClient(), sender, nil, slog.New(slog.NewTextHandler(io.Discard, nil)), Config{
		DeliveryParallelism: 2,
		DeliverySendTimeout: time.Second,
	})
	rows := []domain.YouTubeNotificationDelivery{
		{ID: 41, OutboxID: 401, RoomID: "room-1"},
		{ID: 42, OutboxID: 402, RoomID: "room-2"},
	}
	outboxByID := map[int64]domain.YouTubeNotificationOutbox{
		401: {ID: 401, ChannelID: "UCvideo", Kind: domain.OutboxKindNewVideo, ContentID: "video:1", Payload: `{"video_id":"v1","title":"video 1"}`},
		402: {ID: 402, ChannelID: "UCshort", Kind: domain.OutboxKindNewShort, ContentID: "short:1", Payload: `{"video_id":"s1","title":"short 1"}`},
	}

	done := make(chan deliveryDispatchResult, 1)
	go func() {
		done <- dispatcher.dispatchDeliveryRows(context.Background(), rows, outboxByID)
	}()

	sender.awaitEntered(t)
	select {
	case <-sender.entered:
		t.Fatal("second Karing send started before first send was released")
	case <-time.After(30 * time.Millisecond):
	}
	sender.releaseFirst()

	result := <-done
	if len(result.successDeliveryIDs) != 2 {
		t.Fatalf("successDeliveryIDs = %#v, want 2 ids", result.successDeliveryIDs)
	}
	if got := atomic.LoadInt32(&sender.maxActive); got != 1 {
		t.Fatalf("max active Karing sends = %d, want 1", got)
	}
}

type blockingKaringSender struct {
	entered      chan struct{}
	release      chan struct{}
	active       int32
	maxActive    int32
	blockedFirst int32
}

func newBlockingKaringSender() *blockingKaringSender {
	return &blockingKaringSender{
		entered: make(chan struct{}, 2),
		release: make(chan struct{}),
	}
}

func (s *blockingKaringSender) SendMessage(_ context.Context, _ string, _ string) error {
	return nil
}

func (s *blockingKaringSender) SendYouTubeOutboxKaring(ctx context.Context, _ string, _ domain.YouTubeOutboxDispatchPayload) error {
	active := atomic.AddInt32(&s.active, 1)
	defer atomic.AddInt32(&s.active, -1)
	for {
		maxActive := atomic.LoadInt32(&s.maxActive)
		if active <= maxActive || atomic.CompareAndSwapInt32(&s.maxActive, maxActive, active) {
			break
		}
	}
	s.entered <- struct{}{}
	if atomic.CompareAndSwapInt32(&s.blockedFirst, 0, 1) {
		select {
		case <-s.release:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

func (s *blockingKaringSender) awaitEntered(t *testing.T) {
	t.Helper()
	select {
	case <-s.entered:
	case <-time.After(time.Second):
		t.Fatal("first Karing send did not start")
	}
}

func (s *blockingKaringSender) releaseFirst() {
	close(s.release)
}
