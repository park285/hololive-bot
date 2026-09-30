package egress

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/park285/iris-client-go/v3/iris"
	"github.com/park285/shared-go/v2/pkg/kakaoformat"

	"github.com/kapu/hololive-shared/pkg/service/sendoutcome"
)

const replyStatusPollInterval = 250 * time.Millisecond

var (
	// ErrReplyHandoffOutcomeUnknown은 Markdown 발송의 Iris 접수 뒤 Kakao handoff 결과를 확정할 수 없음을 나타냅니다.
	ErrReplyHandoffOutcomeUnknown = sendoutcome.ErrHandoffOutcomeUnknown
	// ErrReplyHandoffFailed는 Markdown 발송에서 Iris가 Kakao handoff 실패를 확정했음을 나타냅니다.
	ErrReplyHandoffFailed = sendoutcome.ErrHandoffFailed
)

// IrisClient는 alarm-worker가 알림 전송과 Markdown handoff 확인에 사용하는 Iris 계약입니다.
// Karing template은 보내지 않습니다(DEC-20260926-hololive-karing-egress-disposition).
type IrisClient interface {
	SendMessage(ctx context.Context, roomID, message string, opts ...iris.SendOption) error
	SendMarkdown(ctx context.Context, roomID, markdown string, opts ...iris.SendOption) (*iris.ReplyAcceptedResponse, error)
	GetReplyStatus(ctx context.Context, requestID string) (*iris.ReplyStatusSnapshot, error)
}

// OpenChat은 Markdown lane을 결정하는 확인된 Kakao 오픈채팅 여부를 제공합니다.
type OpenChat interface {
	OpenChat(ctx context.Context, roomID string) bool
}

// IrisMessageSender는 방 유형별 message를 Iris로 전송합니다. 오픈채팅은 BOT_MARKDOWN_REPLIES에 따라 Markdown,
// 그 밖의 방은 일반 텍스트 경로를 씁니다.
type IrisMessageSender struct {
	client                  IrisClient
	markdownReplies         bool
	markdownRooms           OpenChat
	replyStatusPollInterval time.Duration
}

// IrisMessageSenderOption은 alarm-worker message lane 구성을 적용합니다.
type IrisMessageSenderOption func(*IrisMessageSender)

// WithMarkdownReplies는 확인된 오픈채팅의 Markdown 전송을 설정합니다.
func WithMarkdownReplies(enabled bool) IrisMessageSenderOption {
	return func(sender *IrisMessageSender) {
		sender.markdownReplies = enabled
	}
}

// WithMarkdownRoomChat은 Markdown lane 판정에 사용할 오픈채팅 resolver를 연결합니다.
func WithMarkdownRoomChat(rooms OpenChat) IrisMessageSenderOption {
	return func(sender *IrisMessageSender) {
		sender.markdownRooms = rooms
	}
}

// NewIrisMessageSender는 typed Iris client로 alarm-worker 전송기를 만듭니다.
func NewIrisMessageSender(client IrisClient, opts ...IrisMessageSenderOption) *IrisMessageSender {
	sender := &IrisMessageSender{
		client:                  client,
		replyStatusPollInterval: replyStatusPollInterval,
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
		return s.sendMarkdown(ctx, roomID, message, opts...)
	}

	message = kakaoformat.Render(message)
	if err := s.client.SendMessage(ctx, roomID, message, opts...); err != nil {
		return fmt.Errorf("iris send message: %w", err)
	}

	return nil
}

func (s *IrisMessageSender) sendMarkdown(ctx context.Context, roomID, message string, opts ...iris.SendOption) error {
	accepted, err := s.client.SendMarkdown(ctx, roomID, message, opts...)
	if err != nil {
		return fmt.Errorf("iris send message: %w", err)
	}

	if accepted == nil {
		return fmt.Errorf("%w: markdown admission response is empty", ErrReplyHandoffOutcomeUnknown)
	}

	requestID, err := acceptedReplyRequestID(accepted.Success, accepted.Delivery, accepted.RequestID)
	if err != nil {
		return fmt.Errorf("validate iris markdown admission: %w", err)
	}

	if err := s.waitForReplyHandoff(ctx, requestID); err != nil {
		return fmt.Errorf("confirm iris markdown handoff: %w", err)
	}

	return nil
}

func (s *IrisMessageSender) useMarkdown(ctx context.Context, roomID string) bool {
	return s != nil && s.markdownReplies && s.markdownRooms != nil && s.markdownRooms.OpenChat(ctx, roomID)
}

// SendMessage는 방 유형에 따라 오픈채팅 Markdown 또는 Kakao 일반 텍스트로 전송합니다.
// Markdown은 접수 ID의 handoff 완료까지 확인하며 불명 결과는 성공으로 바꾸지 않습니다.
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

func acceptedReplyRequestID(success bool, delivery, rawRequestID string) (string, error) {
	if !success || !strings.EqualFold(strings.TrimSpace(delivery), "queued") {
		return "", fmt.Errorf("%w: admission response is not queued", ErrReplyHandoffOutcomeUnknown)
	}

	requestID := strings.TrimSpace(rawRequestID)
	if requestID == "" {
		return "", fmt.Errorf("%w: admission response has no request id", ErrReplyHandoffOutcomeUnknown)
	}

	return requestID, nil
}

func (s *IrisMessageSender) waitForReplyHandoff(ctx context.Context, requestID string) error {
	interval := s.replyStatusPollInterval
	if interval <= 0 {
		interval = replyStatusPollInterval
	}

	ticks := time.Tick(interval)

	for {
		status, pollErr := s.client.GetReplyStatus(ctx, requestID)

		complete, statusErr := assessReplyHandoffPoll(requestID, status, pollErr)
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
				ErrReplyHandoffOutcomeUnknown,
				ctx.Err(),
			)
		case <-ticks:
		}
	}
}

func assessReplyHandoffPoll(requestID string, status *iris.ReplyStatusSnapshot, pollErr error) (bool, error) {
	if pollErr != nil {
		return false, nil //nolint:nilerr // 상태 조회 실패는 handoff 실패가 아니므로 bounded context까지 재조회합니다.
	}

	if status == nil {
		return false, fmt.Errorf("%w: reply status response is empty", ErrReplyHandoffOutcomeUnknown)
	}

	if err := validateReplyHandoffStatus(requestID, status); err != nil {
		return false, err
	}

	return normalizedReplyState(status.State) == "handoff_completed", nil
}

func validateReplyHandoffStatus(requestID string, status *iris.ReplyStatusSnapshot) error {
	if status == nil {
		return fmt.Errorf("%w: reply status response is empty", ErrReplyHandoffOutcomeUnknown)
	}

	if strings.TrimSpace(status.RequestID) != requestID {
		return fmt.Errorf("%w: reply status request id does not match", ErrReplyHandoffOutcomeUnknown)
	}

	switch normalizedReplyState(status.State) {
	case "queued", "preparing", "prepared", "sending", "handoff_completed":
		return nil
	case "failed":
		return ErrReplyHandoffFailed
	case "outcome_unknown":
		return ErrReplyHandoffOutcomeUnknown
	default:
		return fmt.Errorf("%w: reply status state is not recognized", ErrReplyHandoffOutcomeUnknown)
	}
}

func normalizedReplyState(state string) string {
	return strings.ToLower(strings.TrimSpace(state))
}
