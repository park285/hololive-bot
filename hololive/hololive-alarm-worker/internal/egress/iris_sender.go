package egress

import (
	"context"
	"fmt"
	"strings"

	"github.com/park285/iris-client-go/v5/iris"

	"github.com/kapu/hololive-alarm-worker/internal/egress/sendoutcome"
)

type IrisClient interface {
	SendMessageAccepted(ctx context.Context, roomID, message string, opts ...iris.SendOption) (*iris.ReplyAcceptedResponse, error)
	SendRichText(ctx context.Context, roomID, markdown string, opts ...iris.SendOption) (*iris.ReplyAcceptedResponse, error)
}

type OpenChat interface {
	OpenChat(ctx context.Context, roomID string) bool
}

type IrisMessageSender struct {
	client          IrisClient
	markdownReplies bool
	markdownRooms   OpenChat
}

type IrisMessageSenderOption func(*IrisMessageSender)

func WithMarkdownReplies(enabled bool) IrisMessageSenderOption {
	return func(sender *IrisMessageSender) {
		sender.markdownReplies = enabled
	}
}

func WithMarkdownRoomChat(rooms OpenChat) IrisMessageSenderOption {
	return func(sender *IrisMessageSender) {
		sender.markdownRooms = rooms
	}
}

func NewIrisMessageSender(client IrisClient, opts ...IrisMessageSenderOption) *IrisMessageSender {
	sender := &IrisMessageSender{client: client}

	for _, option := range opts {
		if option != nil {
			option(sender)
		}
	}

	return sender
}

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

func (s *IrisMessageSender) sendRichText(ctx context.Context, roomID, message string, opts ...iris.SendOption) (string, error) {
	accepted, err := s.client.SendRichText(ctx, roomID, message, opts...)
	if err != nil {
		return "", fmt.Errorf("iris send message: %w", err)
	}

	if accepted == nil {
		return "", fmt.Errorf("%w: richtext admission response is empty", sendoutcome.ErrAdmissionOutcomeUnknown)
	}

	requestID, err := acceptedReplyRequestID(accepted.Success, accepted.Delivery, accepted.RequestID)
	if err != nil {
		return "", fmt.Errorf("validate iris richtext admission: %w", err)
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

func admissionRequestID(rawRequestID string) (string, error) {
	requestID := strings.TrimSpace(rawRequestID)
	if requestID == "" {
		return "", fmt.Errorf("%w: admission response has no request id", sendoutcome.ErrAdmissionOutcomeUnknown)
	}

	return requestID, nil
}
