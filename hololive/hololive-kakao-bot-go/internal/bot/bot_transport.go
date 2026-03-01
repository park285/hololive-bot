package bot

import (
	"context"
	"fmt"

	"github.com/kapu/hololive-shared/pkg/adapter"
	"github.com/kapu/hololive-shared/pkg/constants"
	appErrors "github.com/kapu/hololive-shared/pkg/errors"
	"github.com/kapu/hololive-shared/pkg/iris"
)

// CommandTransport: 명령 실행 결과(텍스트/이미지/에러)를 Iris로 전달합니다.
type CommandTransport struct {
	irisClient iris.Client
	formatter  *adapter.ResponseFormatter
}

func NewCommandTransport(irisClient iris.Client, formatter *adapter.ResponseFormatter) *CommandTransport {
	return &CommandTransport{
		irisClient: irisClient,
		formatter:  formatter,
	}
}

func (t *CommandTransport) SendMessage(ctx context.Context, room, message string) error {
	if t == nil || t.irisClient == nil {
		return fmt.Errorf("send message: iris client is not configured")
	}

	sendCtx, cancel := context.WithTimeout(ctx, constants.RequestTimeout.BotCommand)
	defer cancel()

	if err := t.irisClient.SendMessage(sendCtx, room, message); err != nil {
		serviceErr := appErrors.NewServiceError("failed to send message", "iris", "send_message", err)
		return fmt.Errorf("send message to room %s: %w", room, serviceErr)
	}
	return nil
}

func (t *CommandTransport) SendImage(ctx context.Context, room, imageBase64 string) error {
	if t == nil || t.irisClient == nil {
		return fmt.Errorf("send image: iris client is not configured")
	}

	sendCtx, cancel := context.WithTimeout(ctx, constants.RequestTimeout.BotCommand)
	defer cancel()

	if err := t.irisClient.SendImage(sendCtx, room, imageBase64); err != nil {
		serviceErr := appErrors.NewServiceError("failed to send image", "iris", "send_image", err)
		return fmt.Errorf("send image to room %s: %w", room, serviceErr)
	}
	return nil
}

func (t *CommandTransport) SendError(ctx context.Context, room, errorMsg string) error {
	message := errorMsg
	if t != nil && t.formatter != nil {
		message = t.formatter.FormatError(errorMsg)
	}

	if err := t.SendMessage(ctx, room, message); err != nil {
		return fmt.Errorf("send error message: %w", err)
	}
	return nil
}
