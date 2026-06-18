package egress

import (
	"context"
	"fmt"
	"strings"

	"github.com/park285/iris-client-go/iris"
)

type IrisClient interface {
	SendMessage(ctx context.Context, roomID, message string, opts ...iris.SendOption) error
	SendKaringContentList(ctx context.Context, req iris.KaringContentListRequest) (*iris.KaringDryRunResponse, error)
}

type IrisSender interface {
	SendMessage(ctx context.Context, roomID, message string, opts ...iris.SendOption) error
	SendKaringContentList(ctx context.Context, req *iris.KaringContentListRequest) (*iris.KaringDryRunResponse, error)
}

type irisSenderAdapter struct {
	client IrisClient
}

func (a irisSenderAdapter) SendMessage(ctx context.Context, roomID, message string, opts ...iris.SendOption) error {
	return a.client.SendMessage(ctx, roomID, message, opts...)
}

func (a irisSenderAdapter) SendKaringContentList(ctx context.Context, req *iris.KaringContentListRequest) (*iris.KaringDryRunResponse, error) {
	return a.client.SendKaringContentList(ctx, *req)
}

type IrisMessageSender struct {
	client IrisSender
}

func NewIrisMessageSender(client any) *IrisMessageSender {
	switch c := client.(type) {
	case nil:
		return &IrisMessageSender{}
	case IrisSender:
		return &IrisMessageSender{client: c}
	case IrisClient:
		return &IrisMessageSender{client: irisSenderAdapter{client: c}}
	default:
		return &IrisMessageSender{}
	}
}

func (s *IrisMessageSender) SendMessage(ctx context.Context, roomID, message string) error {
	if s == nil || s.client == nil {
		return fmt.Errorf("iris message sender: client is nil")
	}
	if err := s.client.SendMessage(ctx, roomID, message); err != nil {
		return fmt.Errorf("iris send message: %w", err)
	}
	return nil
}

func (s *IrisMessageSender) SendMessageWithClientRequestID(ctx context.Context, roomID, message, clientRequestID string) error {
	if s == nil || s.client == nil {
		return fmt.Errorf("iris message sender: client is nil")
	}
	if err := s.client.SendMessage(ctx, roomID, message, iris.WithClientRequestID(clientRequestID)); err != nil {
		return fmt.Errorf("iris send message: %w", err)
	}
	return nil
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
