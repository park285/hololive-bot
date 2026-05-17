package egress

import (
	"context"
	"fmt"
	"strings"

	"github.com/park285/iris-client-go/iris"
)

type IrisSender interface {
	SendMessage(ctx context.Context, roomID, message string, opts ...iris.SendOption) error
	SendKaringContentList(ctx context.Context, req iris.KaringContentListRequest) (*iris.KaringDryRunResponse, error)
}

type IrisMessageSender struct {
	client IrisSender
}

func NewIrisMessageSender(client IrisSender) *IrisMessageSender {
	return &IrisMessageSender{client: client}
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

func (s *IrisMessageSender) SendKaringContentList(ctx context.Context, roomID string, req iris.KaringContentListRequest) error {
	if s == nil || s.client == nil {
		return fmt.Errorf("iris message sender: client is nil")
	}
	if strings.TrimSpace(req.ReceiverName) == "" && req.ReceiverRoomID == 0 {
		req.ReceiverName = roomID
	}
	if _, err := s.client.SendKaringContentList(ctx, req); err != nil {
		return fmt.Errorf("iris send karing content list: %w", err)
	}
	return nil
}
