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
	regularChatResolver
	SendMessage(ctx context.Context, roomID, message string) error
	SendMessageWithClientRequestID(ctx context.Context, roomID, message, clientRequestID string) error
	SendKaringContentList(ctx context.Context, roomID string, req *iris.KaringContentListRequest) error
}

type YouTubeOutboxKaringSender struct {
	sender         YouTubeOutboxKaringInnerSender
	messageStrings *messagestrings.Store
}

// RegularChat은 inner sender가 확인한 일반채팅 eligibility를 전달합니다.
func (s YouTubeOutboxKaringSender) RegularChat(ctx context.Context, roomID string) bool {
	return s.sender != nil && s.sender.RegularChat(ctx, roomID)
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
		return err
	}

	if err := s.sender.SendMessage(ctx, roomID, message); err != nil {
		return fmt.Errorf("send message: %w", err)
	}

	return nil
}

func (s YouTubeOutboxKaringSender) SendMessageWithClientRequestID(ctx context.Context, roomID, message, clientRequestID string) error {
	if err := s.requireSender(); err != nil {
		return err
	}

	if err := s.sender.SendMessageWithClientRequestID(ctx, roomID, message, clientRequestID); err != nil {
		return fmt.Errorf("send message with client request ID: %w", err)
	}

	return nil
}

// PrepareYouTubeOutboxKaring는 provider 호출 없이 한 chunk의 payload를 요청으로 변환한다.
func (s YouTubeOutboxKaringSender) PrepareYouTubeOutboxKaring(ctx context.Context, roomID string, payload *domain.YouTubeOutboxDispatchPayload, clientRequestID string) (*iris.KaringContentListRequest, error) {
	if err := s.requireSender(); err != nil {
		return nil, err
	}

	if payload == nil {
		return nil, errors.New("youtube outbox karing sender: payload is nil")
	}

	if len(payload.Items) > alarmDispatchKaringMaxItemsPerRequest {
		return nil, errors.New("youtube outbox karing sender: payload exceeds one chunk")
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
		return nil, fmt.Errorf("build youtube outbox karing request: %w", err)
	}

	if len(requests) != 1 || clientRequestID == "" {
		return nil, errors.New("youtube outbox karing request must be one identified chunk")
	}

	requests[0].ClientRequestID = new(clientRequestID)

	return &requests[0], nil
}

// SendYouTubeOutboxKaring는 준비된 한 chunk만 보내며 분할/재시도하지 않는다.
func (s YouTubeOutboxKaringSender) SendYouTubeOutboxKaring(ctx context.Context, roomID string, request *iris.KaringContentListRequest) error {
	if err := s.requireSender(); err != nil {
		return err
	}

	if request == nil {
		return errors.New("youtube outbox karing request is nil")
	}

	if err := s.sender.SendKaringContentList(ctx, roomID, request); err != nil {
		return fmt.Errorf("send youtube outbox karing chunk: %w", err)
	}

	return nil
}
