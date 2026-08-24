package egress

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/park285/iris-client-go/v2/iris"
	"github.com/park285/shared-go/v2/pkg/kakaoformat"
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
	if err := a.client.SendMessage(ctx, roomID, message, opts...); err != nil {
		return fmt.Errorf("send message: %w", err)
	}

	return nil
}

func (a irisSenderAdapter) SendMarkdown(ctx context.Context, roomID, markdown string, opts ...iris.SendOption) (*iris.ReplyAcceptedResponse, error) {
	out, err := a.client.SendMarkdown(ctx, roomID, markdown, opts...)
	if err != nil {
		return nil, fmt.Errorf("send markdown: %w", err)
	}

	return out, nil
}

func (a irisSenderAdapter) SendKaringContentList(ctx context.Context, req *iris.KaringContentListRequest) (*iris.KaringDryRunResponse, error) {
	out, err := a.client.SendKaringContentList(ctx, *req)
	if err != nil {
		return nil, fmt.Errorf("send karing content list: %w", err)
	}

	return out, nil
}

type RoomChat interface {
	OpenChat(ctx context.Context, roomID string) bool
}

type IrisMessageSender struct {
	client          IrisSender
	markdownReplies bool
	rooms           RoomChat
}

type IrisMessageSenderOption func(*IrisMessageSender)

func WithMarkdownReplies(enabled bool) IrisMessageSenderOption {
	return func(s *IrisMessageSender) {
		s.markdownReplies = enabled
	}
}

func WithRoomChat(rooms RoomChat) IrisMessageSenderOption {
	return func(s *IrisMessageSender) {
		s.rooms = rooms
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
	if s == nil || !s.markdownReplies {
		return false
	}

	if s.rooms == nil {
		return false
	}

	return s.rooms.OpenChat(ctx, roomID)
}

func (s *IrisMessageSender) SendMessage(ctx context.Context, roomID, message string) error {
	if s == nil || s.client == nil {
		return errors.New("iris message sender: client is nil")
	}

	if err := s.send(ctx, roomID, message); err != nil {
		return fmt.Errorf("send: %w", err)
	}

	return nil
}

func (s *IrisMessageSender) SendMessageWithClientRequestID(ctx context.Context, roomID, message, clientRequestID string) error {
	if s == nil || s.client == nil {
		return errors.New("iris message sender: client is nil")
	}

	if err := s.send(ctx, roomID, message, iris.WithClientRequestID(clientRequestID)); err != nil {
		return fmt.Errorf("send: %w", err)
	}

	return nil
}

func (s *IrisMessageSender) SendKaringContentList(ctx context.Context, roomID string, req *iris.KaringContentListRequest) error {
	if s == nil || s.client == nil {
		return errors.New("iris message sender: client is nil")
	}

	if req == nil {
		return errors.New("iris message sender: karing request is nil")
	}

	if strings.TrimSpace(req.ReceiverName) == "" && req.ReceiverRoomID == 0 {
		req.ReceiverName = roomID
	}

	if _, err := s.client.SendKaringContentList(ctx, req); err != nil {
		return fmt.Errorf("iris send karing content list: %w", err)
	}

	return nil
}
