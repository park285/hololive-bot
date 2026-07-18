package dispatchrun

import (
	"context"
	"fmt"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
	"github.com/park285/iris-client-go/iris"
)

type YouTubeOutboxKaringInnerSender interface {
	SendMessage(ctx context.Context, roomID, message string) error
	SendMessageWithClientRequestID(ctx context.Context, roomID, message, clientRequestID string) error
	SendKaringContentList(ctx context.Context, roomID string, req *iris.KaringContentListRequest) error
}

type YouTubeOutboxKaringSender struct {
	sender         YouTubeOutboxKaringInnerSender
	messageStrings *messagestrings.Store
}

func NewYouTubeOutboxKaringSender(sender YouTubeOutboxKaringInnerSender, messageStrings *messagestrings.Store) YouTubeOutboxKaringSender {
	return YouTubeOutboxKaringSender{sender: sender, messageStrings: messageStrings}
}

func (s YouTubeOutboxKaringSender) requireSender() error {
	if s.sender == nil {
		return fmt.Errorf("youtube outbox karing sender: sender is nil")
	}
	return nil
}

func (s YouTubeOutboxKaringSender) SendMessage(ctx context.Context, roomID, message string) error {
	if err := s.requireSender(); err != nil {
		return err
	}
	return s.sender.SendMessage(ctx, roomID, message)
}

func (s YouTubeOutboxKaringSender) SendMessageWithClientRequestID(ctx context.Context, roomID, message, clientRequestID string) error {
	if err := s.requireSender(); err != nil {
		return err
	}
	return s.sender.SendMessageWithClientRequestID(ctx, roomID, message, clientRequestID)
}

func (s YouTubeOutboxKaringSender) SendYouTubeOutboxKaring(ctx context.Context, roomID string, payload *domain.YouTubeOutboxDispatchPayload) error {
	if err := s.requireSender(); err != nil {
		return err
	}
	if payload == nil {
		return fmt.Errorf("youtube outbox karing sender: payload is nil")
	}
	envelope := domain.AlarmQueueEnvelope{
		Notification: domain.AlarmNotification{
			RoomID:    roomID,
			AlarmType: payload.AlarmType,
		},
		SourceKind:    domain.AlarmDispatchSourceKindYouTubeOutbox,
		YouTubeOutbox: payload,
		Version:       1,
	}
	requests, err := buildAlarmDispatchKaringContentListRequests(ctx, s.messageStrings, alarmDispatchGroup{
		roomID:    roomID,
		envelopes: []domain.AlarmQueueEnvelope{envelope},
	})
	if err != nil {
		return fmt.Errorf("build youtube outbox karing request: %w", err)
	}
	for i := range requests {
		if err := s.sender.SendKaringContentList(ctx, roomID, &requests[i]); err != nil {
			return fmt.Errorf("send youtube outbox karing request %d: %w", i, err)
		}
	}
	return nil
}
