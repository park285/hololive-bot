package runtime

import (
	"context"
	"errors"
	"fmt"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/alarm/queue"
	"github.com/kapu/hololive-shared/pkg/service/delivery"
)

type deliveryDispatchPublisher struct {
	publisher *queue.Publisher
}

func (p deliveryDispatchPublisher) PublishPending(ctx context.Context, items []delivery.OutboxItem) error {
	if p.publisher == nil {
		return errors.New("publish delivery dispatch: publisher is nil")
	}

	_, err := p.publisher.PublishDispatchBatch(ctx, deliveryDispatchEnvelopes(items))
	if err != nil {
		return fmt.Errorf("publish delivery dispatch: %w", err)
	}

	return nil
}

func (p deliveryDispatchPublisher) PublishShadow(ctx context.Context, items []delivery.OutboxItem) error {
	if p.publisher == nil {
		return errors.New("publish delivery dispatch shadow: publisher is nil")
	}

	_, err := p.publisher.PublishShadowDispatchBatch(ctx, deliveryDispatchEnvelopes(items))
	if err != nil {
		return fmt.Errorf("publish delivery dispatch shadow: %w", err)
	}

	return nil
}

func deliveryDispatchEnvelopes(items []delivery.OutboxItem) []domain.AlarmQueueEnvelope {
	envelopes := make([]domain.AlarmQueueEnvelope, 0, len(items))
	for i := range items {
		payload := domain.DeliveryDigestDispatchPayload{
			Kind:               items[i].Kind,
			PeriodKey:          items[i].PeriodKey,
			PreRenderedMessage: items[i].Message,
		}

		envelopes = append(envelopes, domain.AlarmQueueEnvelope{
			Notification: domain.AlarmNotification{
				AlarmType: domain.AlarmTypeCommunity,
				RoomID:    items[i].RoomID,
			},
			SourceKind:     domain.AlarmDispatchSourceKindDeliveryDigest,
			DeliveryDigest: &payload,
			Version:        1,
		})
	}

	return envelopes
}
