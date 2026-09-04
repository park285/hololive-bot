package egress

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/park285/iris-client-go/v2/iris"
	"github.com/park285/shared-go/v2/pkg/kakaoformat"
)

const karingStatusPollInterval = 250 * time.Millisecond

var (
	// ErrKaringOutcomeUnknown은 Iris 접수 뒤 Kakao handoff 결과를 확정할 수 없음을 나타냅니다.
	ErrKaringOutcomeUnknown = errors.New("iris karing outcome unknown")
	// ErrKaringStatusFailed는 Iris가 Kakao handoff 실패를 확정했음을 나타냅니다.
	ErrKaringStatusFailed = errors.New("iris karing handoff failed")
)

// IrisClient는 alarm-worker가 알림 전송과 Karing handoff 확인에 사용하는 Iris 계약입니다.
type IrisClient interface {
	SendMessage(ctx context.Context, roomID, message string, opts ...iris.SendOption) error
	SendMarkdown(ctx context.Context, roomID, markdown string, opts ...iris.SendOption) (*iris.ReplyAcceptedResponse, error)
	SendKaringContentList(ctx context.Context, req iris.KaringContentListRequest) (*iris.KaringDryRunResponse, error)
	GetReplyStatus(ctx context.Context, requestID string) (*iris.ReplyStatusSnapshot, error)
}

// RoomChat은 Karing과 Markdown lane을 결정하는 확인된 Kakao 방 유형을 제공합니다.
type RoomChat interface {
	OpenChat(ctx context.Context, roomID string) bool
	RegularChat(ctx context.Context, roomID string) bool
}

// IrisMessageSender는 방 유형별 message와 확인 가능한 Karing 알림을 Iris로 전송합니다.
type IrisMessageSender struct {
	client                   IrisClient
	markdownReplies          bool
	rooms                    RoomChat
	karingStatusPollInterval time.Duration
}

// IrisMessageSenderOption은 alarm-worker message lane 구성을 적용합니다.
type IrisMessageSenderOption func(*IrisMessageSender)

// WithMarkdownReplies는 확인된 오픈채팅의 Markdown 전송을 설정합니다.
func WithMarkdownReplies(enabled bool) IrisMessageSenderOption {
	return func(sender *IrisMessageSender) {
		sender.markdownReplies = enabled
	}
}

// WithRoomChat은 방 유형 정본을 sender에 연결합니다.
func WithRoomChat(rooms RoomChat) IrisMessageSenderOption {
	return func(sender *IrisMessageSender) {
		sender.rooms = rooms
	}
}

// NewIrisMessageSender는 typed Iris client로 alarm-worker 전송기를 만듭니다.
func NewIrisMessageSender(client IrisClient, opts ...IrisMessageSenderOption) *IrisMessageSender {
	sender := &IrisMessageSender{
		client:                   client,
		karingStatusPollInterval: karingStatusPollInterval,
	}

	for _, option := range opts {
		if option != nil {
			option(sender)
		}
	}

	return sender
}

func (s *IrisMessageSender) send(ctx context.Context, roomID, message string, opts ...iris.SendOption) error {
	if s.useMarkdown(ctx, roomID) {
		if _, err := s.client.SendMarkdown(ctx, roomID, message, opts...); err != nil {
			return fmt.Errorf("iris send message: %w", err)
		}

		return nil
	}

	message = kakaoformat.Render(message)
	if err := s.client.SendMessage(ctx, roomID, message, opts...); err != nil {
		return fmt.Errorf("iris send message: %w", err)
	}

	return nil
}

func (s *IrisMessageSender) useMarkdown(ctx context.Context, roomID string) bool {
	return s != nil && s.markdownReplies && s.rooms != nil && s.rooms.OpenChat(ctx, roomID)
}

// RegularChat은 room facts로 확인된 일반채팅인지 반환합니다.
func (s *IrisMessageSender) RegularChat(ctx context.Context, roomID string) bool {
	return s != nil && s.rooms != nil && s.rooms.RegularChat(ctx, roomID)
}

// SendMessage는 방 유형에 따라 오픈채팅 Markdown 또는 Kakao 일반 텍스트로 전송합니다.
func (s *IrisMessageSender) SendMessage(ctx context.Context, roomID, message string) error {
	if s == nil || s.client == nil {
		return errors.New("iris message sender: client is nil")
	}

	if err := s.send(ctx, roomID, message); err != nil {
		return fmt.Errorf("send: %w", err)
	}

	return nil
}

// SendMessageWithClientRequestID는 방 유형별 message lane에 Iris 멱등성 ID를 포함합니다.
func (s *IrisMessageSender) SendMessageWithClientRequestID(ctx context.Context, roomID, message, clientRequestID string) error {
	if s == nil || s.client == nil {
		return errors.New("iris message sender: client is nil")
	}

	if err := s.send(ctx, roomID, message, iris.WithClientRequestID(clientRequestID)); err != nil {
		return fmt.Errorf("send: %w", err)
	}

	return nil
}

// SendKaringContentList는 Iris 접수 뒤 exact request ID가 handoff_completed가 될 때까지 확인합니다.
func (s *IrisMessageSender) SendKaringContentList(ctx context.Context, roomID string, req *iris.KaringContentListRequest) error {
	if s == nil || s.client == nil {
		return errors.New("iris message sender: client is nil")
	}

	if req == nil {
		return errors.New("iris message sender: karing request is nil")
	}

	request := *req
	if strings.TrimSpace(request.ReceiverName) == "" && request.ReceiverRoomID == 0 {
		request.ReceiverName = roomID
	}

	accepted, err := s.client.SendKaringContentList(ctx, request)
	if err != nil {
		return fmt.Errorf("iris send karing content list: %w", err)
	}

	requestID, err := acceptedKaringRequestID(accepted)
	if err != nil {
		return fmt.Errorf("validate iris karing admission: %w", err)
	}

	if err := s.waitForKaringHandoff(ctx, requestID); err != nil {
		return fmt.Errorf("confirm iris karing handoff: %w", err)
	}

	return nil
}

func acceptedKaringRequestID(accepted *iris.KaringDryRunResponse) (string, error) {
	if accepted == nil {
		return "", fmt.Errorf("%w: admission response is empty", ErrKaringOutcomeUnknown)
	}

	if !accepted.Success || !strings.EqualFold(strings.TrimSpace(accepted.Delivery), "queued") {
		return "", fmt.Errorf("%w: admission response is not queued", ErrKaringOutcomeUnknown)
	}

	requestID := strings.TrimSpace(accepted.RequestID)
	if requestID == "" {
		return "", fmt.Errorf("%w: admission response has no request id", ErrKaringOutcomeUnknown)
	}

	return requestID, nil
}

func (s *IrisMessageSender) waitForKaringHandoff(ctx context.Context, requestID string) error {
	interval := s.karingStatusPollInterval
	if interval <= 0 {
		interval = karingStatusPollInterval
	}

	ticks := time.Tick(interval)

	for {
		status, pollErr := s.client.GetReplyStatus(ctx, requestID)

		complete, statusErr := assessKaringHandoffPoll(requestID, status, pollErr)
		if statusErr != nil {
			return statusErr
		}

		if complete {
			return nil
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf(
				"%w: status polling ended before handoff: %w",
				ErrKaringOutcomeUnknown,
				ctx.Err(),
			)
		case <-ticks:
		}
	}
}

func assessKaringHandoffPoll(requestID string, status *iris.ReplyStatusSnapshot, pollErr error) (bool, error) {
	if pollErr != nil {
		return false, nil //nolint:nilerr // 상태 조회 실패는 handoff 실패가 아니므로 bounded context까지 재조회합니다.
	}

	if status == nil {
		return false, fmt.Errorf("%w: reply status response is empty", ErrKaringOutcomeUnknown)
	}

	if err := validateKaringReplyStatus(requestID, status); err != nil {
		return false, err
	}

	return karingReplyState(status.State) == "handoff_completed", nil
}

func validateKaringReplyStatus(requestID string, status *iris.ReplyStatusSnapshot) error {
	if status == nil {
		return fmt.Errorf("%w: reply status response is empty", ErrKaringOutcomeUnknown)
	}

	if strings.TrimSpace(status.RequestID) != requestID {
		return fmt.Errorf("%w: reply status request id does not match", ErrKaringOutcomeUnknown)
	}

	switch karingReplyState(status.State) {
	case "queued", "preparing", "prepared", "sending", "handoff_completed":
		return nil
	case "failed":
		return ErrKaringStatusFailed
	case "outcome_unknown":
		return ErrKaringOutcomeUnknown
	default:
		return fmt.Errorf("%w: reply status state is not recognized", ErrKaringOutcomeUnknown)
	}
}

func karingReplyState(state string) string {
	return strings.ToLower(strings.TrimSpace(state))
}
