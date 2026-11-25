package egress

import (
	"context"
	"errors"
	"fmt"

	"github.com/park285/iris-client-go/v6/iris"
	"github.com/park285/shared-go/v2/pkg/kakaoformat"

	"github.com/kapu/hololive-shared/pkg/service/delivery"
)

func (s *IrisMessageSender) PrepareMessageRequest(ctx context.Context, roomID string, message delivery.MessageVariants) (body, route string, err error) {
	if s == nil || s.client == nil {
		return "", "", errors.New("iris message sender: client is nil")
	}

	if s.useMarkdown(ctx, roomID) {
		return message.RichText, "richtext", nil
	}

	return kakaoformat.Render(message.Plain), "text", nil
}

func (s *IrisMessageSender) SendPreparedMessage(ctx context.Context, roomID, body, route, clientRequestID string) (string, error) {
	if s == nil || s.client == nil {
		return "", errors.New("iris message sender: client is nil")
	}

	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("send prepared message: %w", err)
	}

	switch route {
	case "richtext":
		return s.sendRichText(ctx, roomID, body, iris.WithClientRequestID(clientRequestID))
	case "text":
		requestID, err := s.sendText(ctx, roomID, body, iris.WithClientRequestID(clientRequestID))
		if err != nil {
			return "", fmt.Errorf("send prepared text: %w", err)
		}

		return requestID, nil
	default:
		return "", errors.New("send prepared message: unsupported route")
	}
}
