package youtubedispatch

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/alarm/handoff"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/dispatchstate"
)

type youtubeOutboxHandoffTestPublisher struct {
	pending []domain.YouTubeOutboxDispatchPayload
	shadow  []domain.YouTubeOutboxDispatchPayload
}

func (p *youtubeOutboxHandoffTestPublisher) PublishPending(_ context.Context, _ string, payload *domain.YouTubeOutboxDispatchPayload) error {
	p.pending = append(p.pending, *payload)
	return nil
}

func (p *youtubeOutboxHandoffTestPublisher) PublishShadow(_ context.Context, _ string, payload *domain.YouTubeOutboxDispatchPayload) error {
	p.shadow = append(p.shadow, *payload)
	return nil
}

func TestDispatcherCutoverHandsOffMilestoneWithoutDirectSend(t *testing.T) {
	t.Parallel()

	sender := &youtubeOutboxKaringTestSender{}
	publisher := &youtubeOutboxHandoffTestPublisher{}
	dispatcher := NewDispatcher(nil, cachemocks.NewLenientClient(), sender, newSendTestRenderer(t), slog.New(slog.NewTextHandler(io.Discard, nil)), &dispatchstate.Config{
		DeliveryParallelism: 1,
		DeliverySendTimeout: time.Second,
	})
	if err := dispatcher.ConfigureHandoff(handoff.ModeCutover, publisher); err != nil {
		t.Fatalf("ConfigureHandoff() error = %v", err)
	}
	rows := []domain.YouTubeNotificationDelivery{{ID: 21, OutboxID: 201, RoomID: "room-1"}}
	outboxByID := map[int64]domain.YouTubeNotificationOutbox{
		201: {ID: 201, ChannelID: "UCmilestone", Kind: domain.OutboxKindMilestone, ContentID: "milestone:1", Payload: `{"milestone":"100만"}`},
	}

	result := dispatcher.send.dispatchDeliveryRows(t.Context(), rows, outboxByID)

	if len(result.SuccessDeliveryIDs) != 1 || len(publisher.pending) != 1 {
		t.Fatalf("result=%#v pending=%d", result, len(publisher.pending))
	}
	if len(sender.messages) != 0 || len(sender.payloads) != 0 {
		t.Fatalf("direct sends messages=%d payloads=%d", len(sender.messages), len(sender.payloads))
	}
}

func TestDispatcherShadowHandoffPreservesDirectKaringSend(t *testing.T) {
	t.Parallel()

	sender := &youtubeOutboxKaringTestSender{}
	publisher := &youtubeOutboxHandoffTestPublisher{}
	dispatcher := NewDispatcher(nil, cachemocks.NewLenientClient(), sender, nil, slog.New(slog.NewTextHandler(io.Discard, nil)), &dispatchstate.Config{
		DeliveryParallelism: 1,
		DeliverySendTimeout: time.Second,
	})
	if err := dispatcher.ConfigureHandoff(handoff.ModeShadow, publisher); err != nil {
		t.Fatalf("ConfigureHandoff() error = %v", err)
	}
	rows := []domain.YouTubeNotificationDelivery{{ID: 31, OutboxID: 301, RoomID: "room-1"}}
	outboxByID := map[int64]domain.YouTubeNotificationOutbox{
		301: {ID: 301, ChannelID: "UCcommunity", Kind: domain.OutboxKindCommunityPost, ContentID: "post:1", Payload: `{"post_id":"1","content_text":"hello"}`},
	}

	result := dispatcher.send.dispatchDeliveryRows(t.Context(), rows, outboxByID)

	if len(result.SuccessDeliveryIDs) != 1 || len(publisher.shadow) != 1 || len(sender.payloads) != 1 {
		t.Fatalf("result=%#v shadow=%d direct=%d", result, len(publisher.shadow), len(sender.payloads))
	}
}
