package egress

import (
	"context"
	"errors"
	"fmt"

	"github.com/park285/iris-client-go/v3/iris"
	"github.com/park285/shared-go/v2/pkg/kakaoformat"
)

// PrepareMessageRequest는 설정과 방 유형을 외부 발송 전 최종 본문·경로로 고정한다.
func (s *IrisMessageSender) PrepareMessageRequest(ctx context.Context, roomID, message string) (body, route string, err error) {
	if s == nil || s.client == nil {
		return "", "", errors.New("iris message sender: client is nil")
	}

	if s.useMarkdown(ctx, roomID) {
		return message, "markdown", nil
	}

	return kakaoformat.Render(message), "text", nil
}

// SendPreparedMessage는 저장된 요청을 재해석하지 않아 설정 변경 뒤에도 같은 ID의 payload를 보존한다.
func (s *IrisMessageSender) SendPreparedMessage(ctx context.Context, roomID, body, route, clientRequestID string) error {
	if s == nil || s.client == nil {
		return errors.New("iris message sender: client is nil")
	}

	if err := ctx.Err(); err != nil {
		return fmt.Errorf("send prepared message: %w", err)
	}

	switch route {
	case "markdown":
		return s.sendMarkdown(ctx, roomID, body, iris.WithClientRequestID(clientRequestID))
	case "text":
		if err := s.client.SendMessage(ctx, roomID, body, iris.WithClientRequestID(clientRequestID)); err != nil {
			return fmt.Errorf("send prepared text: %w", err)
		}

		return nil
	default:
		return errors.New("send prepared message: unsupported route")
	}
}
