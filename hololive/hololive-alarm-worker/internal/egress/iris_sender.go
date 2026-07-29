package egress

import (
	"context"
	"fmt"
	"strings"

	"github.com/park285/iris-client-go/iris"
)

type IrisClient interface {
	SendMessage(ctx context.Context, roomID, message string, opts ...iris.SendOption) error
	SendMarkdown(ctx context.Context, roomID, markdown string, opts ...iris.SendOption) (*iris.ReplyAcceptedResponse, error)
	SendKaringContentList(ctx context.Context, req iris.KaringContentListRequest) (*iris.KaringDryRunResponse, error)
}

type IrisSender interface {
	SendMessage(ctx context.Context, roomID, message string, opts ...iris.SendOption) error
	SendMarkdown(ctx context.Context, roomID, markdown string, opts ...iris.SendOption) (*iris.ReplyAcceptedResponse, error)
	SendKaringContentList(ctx context.Context, req *iris.KaringContentListRequest) (*iris.KaringDryRunResponse, error)
}

type irisSenderAdapter struct {
	client IrisClient
}

func (a irisSenderAdapter) SendMessage(ctx context.Context, roomID, message string, opts ...iris.SendOption) error {
	return a.client.SendMessage(ctx, roomID, message, opts...)
}

func (a irisSenderAdapter) SendMarkdown(ctx context.Context, roomID, markdown string, opts ...iris.SendOption) (*iris.ReplyAcceptedResponse, error) {
	return a.client.SendMarkdown(ctx, roomID, markdown, opts...)
}

func (a irisSenderAdapter) SendKaringContentList(ctx context.Context, req *iris.KaringContentListRequest) (*iris.KaringDryRunResponse, error) {
	return a.client.SendKaringContentList(ctx, *req)
}

type IrisMessageSender struct {
	client          IrisSender
	markdownReplies bool
}

type IrisMessageSenderOption func(*IrisMessageSender)

func WithMarkdownReplies(enabled bool) IrisMessageSenderOption {
	return func(s *IrisMessageSender) {
		s.markdownReplies = enabled
	}
}

func NewIrisMessageSender(client any, opts ...IrisMessageSenderOption) *IrisMessageSender {
	sender := &IrisMessageSender{client: resolveIrisSender(client)}
	for _, opt := range opts {
		if opt != nil {
			opt(sender)
		}
	}
	return sender
}

func resolveIrisSender(client any) IrisSender {
	switch c := client.(type) {
	case nil:
		return nil
	case IrisSender:
		return c
	case IrisClient:
		return irisSenderAdapter{client: c}
	default:
		return nil
	}
}

func (s *IrisMessageSender) send(ctx context.Context, roomID, message string, opts ...iris.SendOption) error {
	if s.markdownReplies {
		if _, err := s.client.SendMarkdown(ctx, roomID, message, opts...); err != nil {
			return fmt.Errorf("iris send message: %w", err)
		}
		return nil
	}
	if err := s.client.SendMessage(ctx, roomID, message, opts...); err != nil {
		return fmt.Errorf("iris send message: %w", err)
	}
	return nil
}

func (s *IrisMessageSender) SendMessage(ctx context.Context, roomID, message string) error {
	if s == nil || s.client == nil {
		return fmt.Errorf("iris message sender: client is nil")
	}
	return s.send(ctx, roomID, message)
}

func (s *IrisMessageSender) SendMessageWithClientRequestID(ctx context.Context, roomID, message, clientRequestID string) error {
	if s == nil || s.client == nil {
		return fmt.Errorf("iris message sender: client is nil")
	}
	return s.send(ctx, roomID, message, iris.WithClientRequestID(clientRequestID))
}

func (s *IrisMessageSender) SendKaringContentList(ctx context.Context, roomID string, req *iris.KaringContentListRequest) error {
	if s == nil || s.client == nil {
		return fmt.Errorf("iris message sender: client is nil")
	}
	if req == nil {
		return fmt.Errorf("iris message sender: karing request is nil")
	}
	if strings.TrimSpace(req.ReceiverName) == "" && req.ReceiverRoomID == 0 {
		req.ReceiverName = roomID
	}
	if _, err := s.client.SendKaringContentList(ctx, req); err != nil {
		return fmt.Errorf("iris send karing content list: %w", err)
	}
	return nil
}
