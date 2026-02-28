package providers

import (
	"context"
	"fmt"

	"github.com/kapu/hololive-shared/pkg/iris"
)

// IrisMessageSender adapts iris.Client to legacy sender interfaces that don't accept SendOption.
type IrisMessageSender struct {
	client iris.Client
}

// NewIrisMessageSender - IrisMessageSender 생성자
func NewIrisMessageSender(client iris.Client) IrisMessageSender {
	return IrisMessageSender{client: client}
}

// SendMessage - iris 클라이언트를 통해 메시지를 발송한다.
func (s IrisMessageSender) SendMessage(ctx context.Context, roomID, message string) error {
	if err := s.client.SendMessage(ctx, roomID, message); err != nil {
		return fmt.Errorf("iris send message: %w", err)
	}
	return nil
}
