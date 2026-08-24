package dispatchrun

import (
	"context"
	"errors"
	"fmt"

	"github.com/park285/iris-client-go/v2/iris"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
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
		return errors.New("youtube outbox karing sender: sender is nil")
	}

	return nil
}

func (s YouTubeOutboxKaringSender) SendMessage(ctx context.Context, roomID, message string) error {
	if err := s.requireSender(); err != nil {
		//nolint:wrapcheck // requireSender가 이미 패키지 이름을 붙인 완결된 오류를 반환하므로, 다시 감싸면 고정된 오류 계약이 깨진다.
		return err
	}

	if err := s.sender.SendMessage(ctx, roomID, message); err != nil {
		return fmt.Errorf("send message: %w", err)
	}

	return nil
}

func (s YouTubeOutboxKaringSender) SendMessageWithClientRequestID(ctx context.Context, roomID, message, clientRequestID string) error {
	if err := s.requireSender(); err != nil {
		//nolint:wrapcheck // requireSender가 이미 패키지 이름을 붙인 완결된 오류를 반환하므로, 다시 감싸면 고정된 오류 계약이 깨진다.
		return err
	}

	if err := s.sender.SendMessageWithClientRequestID(ctx, roomID, message, clientRequestID); err != nil {
		return fmt.Errorf("send message with client request ID: %w", err)
	}

	return nil
}

func (s YouTubeOutboxKaringSender) SendYouTubeOutboxKaring(ctx context.Context, roomID string, payload *domain.YouTubeOutboxDispatchPayload) error {
	if err := s.requireSender(); err != nil {
		//nolint:wrapcheck // requireSender가 이미 패키지 이름을 붙인 완결된 오류를 반환하므로, 다시 감싸면 고정된 오류 계약이 깨진다.
		return err
	}

	if payload == nil {
		return errors.New("youtube outbox karing sender: payload is nil")
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
