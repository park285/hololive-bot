package youtubedispatch

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kapu/hololive-alarm-worker/internal/egress"
	"github.com/kapu/hololive-alarm-worker/internal/service/youtube/outbox/dispatchstate"
	"github.com/kapu/hololive-shared/pkg/domain"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
)

type youtubeOutboxKaringTestSender struct {
	mu         sync.Mutex
	messages   []string
	payloads   []domain.YouTubeOutboxDispatchPayload
	failErr    error
	calls      int
	nonRegular bool
}

func (s *youtubeOutboxKaringTestSender) RegularChat(context.Context, string) bool {
	return !s.nonRegular
}

func (s *youtubeOutboxKaringTestSender) SendMessage(_ context.Context, roomID, message string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.messages = append(s.messages, roomID+":"+message)

	return nil
}

func (s *youtubeOutboxKaringTestSender) SendYouTubeOutboxKaring(_ context.Context, _ string, payload *domain.YouTubeOutboxDispatchPayload) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.calls++

	if s.failErr != nil {
		return s.failErr
	}

	s.payloads = append(s.payloads, *payload)

	return nil
}

func TestSendEngineKaringSenderRequiresRegularChat(t *testing.T) {
	regular := &youtubeOutboxKaringTestSender{}
	regularEngine := &SendEngine{sender: regular}
	_, ok := regularEngine.karingSender(t.Context(), testRoomOne, domain.OutboxKindNewVideo)

	if !ok {
		t.Fatal("known regular chat should use Karing")
	}

	nonRegular := &youtubeOutboxKaringTestSender{nonRegular: true}
	nonRegularEngine := &SendEngine{sender: nonRegular}

	_, ok = nonRegularEngine.karingSender(t.Context(), testRoomOne, domain.OutboxKindNewVideo)

	if ok {
		t.Fatal("open or unknown chat must use the existing message path")
	}
}

func TestSendEngineKaringOutcomeUnknownPreservesSendingWithoutRepost(t *testing.T) {
	t.Parallel()

	sender := &youtubeOutboxKaringTestSender{failErr: egress.ErrKaringOutcomeUnknown}
	engine, claims := newOutcomeUnknownTestEngine(sender, nil, time.Second)
	transition := &lifecycleTransitionSpy{}

	engine.transition = transition

	rows := []domain.YouTubeNotificationDelivery{{ID: 101, OutboxID: 1, RoomID: testRoomOne}}
	outboxes := []domain.YouTubeNotificationOutbox{{
		ID: 1, ChannelID: "UCvideo", Kind: domain.OutboxKindNewVideo,
		ContentID: "video-1", Payload: `{"video_id":"video-1","title":"video 1"}`,
	}}
	claimTokens := []dispatchstate.ClaimToken{outcomeUnknownClaimToken(&outboxes[0])}
	result := dispatchstate.DispatchResult{FailureBuckets: make(map[string][]int64)}

	var mu sync.Mutex

	engine.dispatchClaimedKaring(
		t.Context(), sender, testRoomOne, "UCvideo", domain.OutboxKindNewVideo,
		rows, outboxes, claimTokens, "", &result, &mu,
	)

	assertOutcomeUnknownHold(t, &result, claims)

	if sender.calls != 1 {
		t.Fatalf("Karing post calls = %d, want 1", sender.calls)
	}

	if got := transition.beginCalls.Load(); got != 1 {
		t.Fatalf("begin calls = %d, want 1", got)
	}

	if got := transition.startedFailureCalls.Load(); got != 0 {
		t.Fatalf("started failure calls = %d, want 0", got)
	}

	if got := transition.completeCalls.Load(); got != 0 {
		t.Fatalf("complete calls = %d, want 0", got)
	}
}

func TestDispatcherUsesKaringForSupportedYouTubeOutboxKind(t *testing.T) {
	t.Parallel()

	sender := &youtubeOutboxKaringTestSender{}
	dispatcher := newDispatcherForTest(t, nil, cachemocks.NewLenientClient(), sender, nil, slog.New(slog.DiscardHandler), &dispatchstate.Config{
		DeliveryParallelism: 1,
		DeliverySendTimeout: time.Second,
	})
	rows := []domain.YouTubeNotificationDelivery{
		{ID: 11, OutboxID: 101, RoomID: testRoomOne},
		{ID: 12, OutboxID: 102, RoomID: testRoomOne},
	}
	outboxByID := map[int64]domain.YouTubeNotificationOutbox{
		101: {ID: 101, ChannelID: "UCshorts", Kind: domain.OutboxKindNewShort, ContentID: "a", Payload: `{"canonical_post_id":"short:a","video_id":"a","title":"short a"}`},
		102: {ID: 102, ChannelID: "UCshorts", Kind: domain.OutboxKindNewShort, ContentID: "b", Payload: `{"canonical_post_id":"short:b","video_id":"b","title":"short b"}`},
	}

	result := dispatcher.send.dispatchDeliveryRows(t.Context(), rows, outboxByID)

	if len(result.SuccessDeliveryIDs) != 2 {
		t.Fatalf("successDeliveryIDs = %#v, want 2 ids", result.SuccessDeliveryIDs)
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
	dispatcher := newDispatcherForTest(t, nil, cachemocks.NewLenientClient(), sender, newSendTestRenderer(t), slog.New(slog.DiscardHandler), &dispatchstate.Config{
		DeliveryParallelism: 1,
		DeliverySendTimeout: time.Second,
	})
	rows := []domain.YouTubeNotificationDelivery{{ID: 21, OutboxID: 201, RoomID: testRoomOne}}
	outboxByID := map[int64]domain.YouTubeNotificationOutbox{
		201: {ID: 201, ChannelID: "UCmilestone", Kind: domain.OutboxKindMilestone, ContentID: "milestone:1", Payload: testPayloadMilestone},
	}

	result := dispatcher.send.dispatchDeliveryRows(t.Context(), rows, outboxByID)

	if len(result.SuccessDeliveryIDs) != 1 {
		t.Fatalf("successDeliveryIDs = %#v, want one id", result.SuccessDeliveryIDs)
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
	dispatcher := newDispatcherForTest(t, nil, cachemocks.NewLenientClient(), sender, nil, slog.New(slog.DiscardHandler), &dispatchstate.Config{
		DeliveryParallelism: 1,
		DeliverySendTimeout: time.Second,
	})
	rows := []domain.YouTubeNotificationDelivery{{ID: 31, OutboxID: 301, RoomID: testRoomOne}}
	outboxByID := map[int64]domain.YouTubeNotificationOutbox{
		301: {ID: 301, ChannelID: "UCcommunity", Kind: domain.OutboxKindCommunityPost, ContentID: "1", Payload: `{"canonical_post_id":"community:1","post_id":"1","content_text":"hello"}`},
	}

	result := dispatcher.send.dispatchDeliveryRows(t.Context(), rows, outboxByID)

	if len(result.SuccessDeliveryIDs) != 0 {
		t.Fatalf("successDeliveryIDs = %#v, want none", result.SuccessDeliveryIDs)
	}

	if got := result.FailureBuckets["karing send"]; len(got) != 1 || got[0] != 31 {
		t.Fatalf("karing send failure bucket = %#v, want [31]", got)
	}

	if len(sender.messages) != 0 {
		t.Fatalf("text messages = %#v, want none", sender.messages)
	}
}

func TestDispatcherSerializesKaringSends(t *testing.T) {
	sender := newBlockingKaringSender()
	dispatcher := newDispatcherForTest(t, nil, cachemocks.NewLenientClient(), sender, nil, slog.New(slog.DiscardHandler), &dispatchstate.Config{
		DeliveryParallelism: 2,
		DeliverySendTimeout: time.Second,
	})
	rows := []domain.YouTubeNotificationDelivery{
		{ID: 41, OutboxID: 401, RoomID: testRoomOne},
		{ID: 42, OutboxID: 402, RoomID: "room-2"},
	}
	outboxByID := map[int64]domain.YouTubeNotificationOutbox{
		401: {ID: 401, ChannelID: "UCvideo", Kind: domain.OutboxKindNewVideo, ContentID: "v1", Payload: `{"video_id":"v1","title":"video 1"}`},
		402: {ID: 402, ChannelID: "UCshort", Kind: domain.OutboxKindNewShort, ContentID: "s1", Payload: `{"canonical_post_id":"short:s1","video_id":"s1","title":"short 1"}`},
	}

	done := make(chan dispatchstate.DispatchResult, 1)

	go func() {
		done <- dispatcher.send.dispatchDeliveryRows(t.Context(), rows, outboxByID)
	}()

	select {
	case <-sender.entered:
	case result := <-done:
		t.Fatalf("dispatch completed before first Karing send: %+v", result)
	case <-time.After(time.Second):
		t.Fatal("first Karing send did not start")
	}

	select {
	case <-sender.entered:
		t.Fatal("second Karing send started before first send was released")
	case <-time.After(30 * time.Millisecond):
	}

	sender.releaseFirst()

	result := <-done
	if len(result.SuccessDeliveryIDs) != 2 {
		t.Fatalf("successDeliveryIDs = %#v, want 2 ids", result.SuccessDeliveryIDs)
	}

	if got := sender.maxActive.Load(); got != 1 {
		t.Fatalf("max active Karing sends = %d, want 1", got)
	}
}

func TestSendEngineKaringMutexWaitUsesDeliverySendTimeout(t *testing.T) {
	sender := &youtubeOutboxKaringTestSender{}
	engine := newSendEngine(sender, &MessageFormatter{}, slog.New(slog.DiscardHandler), &dispatchstate.Config{
		DeliverySendTimeout: 20 * time.Millisecond,
	}, nil, newAuditLogger(nil, nil, slog.New(slog.DiscardHandler), &dispatchstate.Config{}, nil), nil)
	engine.karingMu.Lock()

	done := make(chan error, 1)

	go func() {
		done <- engine.sendYouTubeOutboxKaring(t.Context(), sender, "room-timeout", &domain.YouTubeOutboxDispatchPayload{
			OutboxIDs:  []int64{1},
			Kind:       domain.OutboxKindNewVideo,
			AlarmType:  domain.AlarmTypeLive,
			ChannelID:  "UC_timeout",
			MemberName: "member",
			Items: []domain.YouTubeOutboxItem{{
				OutboxID:  1,
				ContentID: "video:timeout",
				Payload:   `{"video_id":"timeout","title":"timeout"}`,
			}},
		})
	}()

	select {
	case err := <-done:
		engine.karingMu.Unlock()

		if err == nil || !strings.Contains(err.Error(), "timed out") {
			t.Fatalf("sendYouTubeOutboxKaring() error = %v, want timeout", err)
		}
	case <-time.After(100 * time.Millisecond):
		engine.karingMu.Unlock()

		err := <-done
		t.Fatalf("sendYouTubeOutboxKaring() waited for mutex without timing out, later error = %v", err)
	}
}

type blockingKaringSender struct {
	entered      chan struct{}
	release      chan struct{}
	active       atomic.Int32
	maxActive    atomic.Int32
	blockedFirst atomic.Int32
}

func newBlockingKaringSender() *blockingKaringSender {
	return &blockingKaringSender{
		entered: make(chan struct{}, 2),
		release: make(chan struct{}),
	}
}

func (s *blockingKaringSender) SendMessage(_ context.Context, _, _ string) error {
	return nil
}

func (*blockingKaringSender) RegularChat(context.Context, string) bool {
	return true
}

func (s *blockingKaringSender) SendYouTubeOutboxKaring(ctx context.Context, _ string, _ *domain.YouTubeOutboxDispatchPayload) error {
	active := s.active.Add(1)
	defer s.active.Add(-1)

	for {
		maxActive := s.maxActive.Load()
		if active <= maxActive || s.maxActive.CompareAndSwap(maxActive, active) {
			break
		}
	}

	s.entered <- struct{}{}

	if s.blockedFirst.CompareAndSwap(0, 1) {
		select {
		case <-s.release:
		case <-ctx.Done():
			if err := ctx.Err(); err != nil {
				return fmt.Errorf("blocked karing send: %w", err)
			}

			return nil
		}
	}

	return nil
}

func (s *blockingKaringSender) releaseFirst() {
	close(s.release)
}