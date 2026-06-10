package workerapp

import (
	"context"
	"fmt"

	"github.com/kapu/hololive-alarm-worker/internal/egress"
	"github.com/kapu/hololive-shared/pkg/domain"
)

type youtubeOutboxKaringSender struct {
	sender *egress.IrisMessageSender
}

func newYouTubeOutboxKaringSender(sender *egress.IrisMessageSender) youtubeOutboxKaringSender {
	return youtubeOutboxKaringSender{sender: sender}
}

func (s youtubeOutboxKaringSender) requireSender() error {
	if s.sender == nil {
		return fmt.Errorf("youtube outbox karing sender: sender is nil")
	}
	return nil
}

func (s youtubeOutboxKaringSender) SendMessage(ctx context.Context, roomID, message string) error {
	if err := s.requireSender(); err != nil {
		return err
	}
	return s.sender.SendMessage(ctx, roomID, message)
}

func (s youtubeOutboxKaringSender) SendMessageWithClientRequestID(ctx context.Context, roomID, message, clientRequestID string) error {
	if err := s.requireSender(); err != nil {
		return err
	}
	return s.sender.SendMessageWithClientRequestID(ctx, roomID, message, clientRequestID)
}

func (s youtubeOutboxKaringSender) SendYouTubeOutboxKaring(ctx context.Context, roomID string, payload domain.YouTubeOutboxDispatchPayload) error {
	if err := s.requireSender(); err != nil {
		return err
	}
	envelope := domain.AlarmQueueEnvelope{
		Notification: domain.AlarmNotification{
			RoomID:    roomID,
			AlarmType: payload.AlarmType,
		},
		SourceKind:    domain.AlarmDispatchSourceKindYouTubeOutbox,
		YouTubeOutbox: &payload,
		Version:       1,
	}
	requests, err := buildAlarmDispatchKaringContentListRequests(alarmDispatchGroup{
		roomID:    roomID,
		envelopes: []domain.AlarmQueueEnvelope{envelope},
	})
	if err != nil {
		return fmt.Errorf("build youtube outbox karing request: %w", err)
	}
	for i := range requests {
		if err := s.sender.SendKaringContentList(ctx, roomID, requests[i]); err != nil {
			return fmt.Errorf("send youtube outbox karing request %d: %w", i, err)
		}
	}
	return nil
}
