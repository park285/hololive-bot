package workerapp

import (
	"context"
	"fmt"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/alarm/queue"
)

type youtubeOutboxDispatchPublisher struct {
	publisher *queue.Publisher
}

func (p youtubeOutboxDispatchPublisher) PublishPending(ctx context.Context, roomID string, payload *domain.YouTubeOutboxDispatchPayload) error {
	if p.publisher == nil {
		return fmt.Errorf("publish youtube outbox dispatch: publisher is nil")
	}
	_, err := p.publisher.PublishDispatchBatch(ctx, []domain.AlarmQueueEnvelope{youtubeOutboxDispatchEnvelope(roomID, payload)})
	return err
}

func (p youtubeOutboxDispatchPublisher) PublishShadow(ctx context.Context, roomID string, payload *domain.YouTubeOutboxDispatchPayload) error {
	if p.publisher == nil {
		return fmt.Errorf("publish youtube outbox dispatch shadow: publisher is nil")
	}
	_, err := p.publisher.PublishShadowDispatchBatch(ctx, []domain.AlarmQueueEnvelope{youtubeOutboxDispatchEnvelope(roomID, payload)})
	return err
}

func youtubeOutboxDispatchEnvelope(roomID string, payload *domain.YouTubeOutboxDispatchPayload) domain.AlarmQueueEnvelope {
	alarmType := domain.AlarmTypeLive
	if payload != nil {
		alarmType = payload.AlarmType
	}
	return domain.AlarmQueueEnvelope{
		Notification: domain.AlarmNotification{
			AlarmType: alarmType,
			RoomID:    roomID,
		},
		SourceKind:    domain.AlarmDispatchSourceKindYouTubeOutbox,
		YouTubeOutbox: payload,
		Version:       1,
	}
}
