package egress

import (
	"context"
	"fmt"
	"strings"

	"github.com/park285/iris-client-go/v4/iris"

	"github.com/kapu/hololive-alarm-worker/internal/egress/sendoutcome"
)

// IrisClient는 alarm-worker가 알림 admission에 사용하는 Iris 계약입니다. 텍스트와 Markdown 모두 202 응답의
// requestId를 받고, handoff 확인은 이 계약이 아니라 각 원장의 handoff reconciler가 맡습니다. Karing template은
// 보내지 않습니다.
type IrisClient interface {
	SendMessageAccepted(ctx context.Context, roomID, message string, opts ...iris.SendOption) (*iris.ReplyAcceptedResponse, error)
	SendMarkdown(ctx context.Context, roomID, markdown string, opts ...iris.SendOption) (*iris.ReplyAcceptedResponse, error)
}

// OpenChat은 Markdown lane을 결정하는 확인된 Kakao 오픈채팅 여부를 제공합니다.
type OpenChat interface {
	OpenChat(ctx context.Context, roomID string) bool
}

// IrisMessageSender는 방 유형별 message를 Iris로 전송합니다. 오픈채팅은 BOT_MARKDOWN_REPLIES에 따라 Markdown,
// 그 밖의 방은 일반 텍스트 경로를 씁니다.
type IrisMessageSender struct {
	client          IrisClient
	markdownReplies bool
	markdownRooms   OpenChat
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
	sender := &IrisMessageSender{client: client}

	for _, option := range opts {
		if option != nil {
			option(sender)
		}
	}

	return sender
}

// sendText는 텍스트 202의 requestId를 돌려준다. 운영 중인 bot plane 텍스트 경로(acceptStoredReply)와 같이 응답과
// requestId만 요구하고 success·delivery 값은 보지 않는다. Iris 계약은 텍스트 202의 delivery 값을 정하지 않는다.
func (s *IrisMessageSender) sendText(ctx context.Context, roomID, message string, opts ...iris.SendOption) (string, error) {
	accepted, err := s.client.SendMessageAccepted(ctx, roomID, message, opts...)
	if err != nil {
		return "", fmt.Errorf("iris send message: %w", err)
	}

	if accepted == nil {
		return "", fmt.Errorf("%w: text admission response is empty", sendoutcome.ErrAdmissionOutcomeUnknown)
	}

	requestID, err := admissionRequestID(accepted.RequestID)
	if err != nil {
		return "", fmt.Errorf("validate iris text admission: %w", err)
	}

	return requestID, nil
}

// sendMarkdown은 Markdown 202가 성공·queued·requestId를 모두 갖추면 handoff를 기다리지 않고 그 requestId를
// 돌려준다.
func (s *IrisMessageSender) sendMarkdown(ctx context.Context, roomID, message string, opts ...iris.SendOption) (string, error) {
	accepted, err := s.client.SendMarkdown(ctx, roomID, message, opts...)
	if err != nil {
		return "", fmt.Errorf("iris send message: %w", err)
	}

	if accepted == nil {
		return "", fmt.Errorf("%w: markdown admission response is empty", sendoutcome.ErrAdmissionOutcomeUnknown)
	}

	requestID, err := acceptedReplyRequestID(accepted.Success, accepted.Delivery, accepted.RequestID)
	if err != nil {
		return "", fmt.Errorf("validate iris markdown admission: %w", err)
	}

	return requestID, nil
}

func (s *IrisMessageSender) useMarkdown(ctx context.Context, roomID string) bool {
	return s != nil && s.markdownReplies && s.markdownRooms != nil && s.markdownRooms.OpenChat(ctx, roomID)
}

func acceptedReplyRequestID(success bool, delivery, rawRequestID string) (string, error) {
	if !success || !strings.EqualFold(strings.TrimSpace(delivery), "queued") {
		return "", fmt.Errorf("%w: admission response is not queued", sendoutcome.ErrAdmissionOutcomeUnknown)
	}

	return admissionRequestID(rawRequestID)
}

// admissionRequestID는 202의 requestId를 공백을 걷어 돌려준다. 비어 있으면 확인할 대상이 없어 결과 불명이다.
func admissionRequestID(rawRequestID string) (string, error) {
	requestID := strings.TrimSpace(rawRequestID)
	if requestID == "" {
		return "", fmt.Errorf("%w: admission response has no request id", sendoutcome.ErrAdmissionOutcomeUnknown)
	}

	return requestID, nil
}
