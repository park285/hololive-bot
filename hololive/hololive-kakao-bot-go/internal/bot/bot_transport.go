// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package bot

import (
	"context"
	"errors"
	"fmt"

	"github.com/kapu/hololive-shared/pkg/constants"
	iris "github.com/park285/iris-client-go/client"

	"github.com/kapu/hololive-kakao-bot-go/internal/adapter"
	appErrors "github.com/kapu/hololive-kakao-bot-go/internal/errors"
)

const serviceNameIris = "iris"

// CommandTransport: 명령 실행 결과(텍스트/이미지/에러)를 Iris로 전달합니다.
type CommandTransport struct {
	irisClient irisClient
	formatter  *adapter.ResponseFormatter
}

func NewCommandTransport(irisClient irisClient, formatter *adapter.ResponseFormatter) *CommandTransport {
	return &CommandTransport{
		irisClient: irisClient,
		formatter:  formatter,
	}
}

func (t *CommandTransport) SendMessage(ctx context.Context, room, message string) error {
	if t == nil || t.irisClient == nil {
		return errors.New("send message: iris client is not configured")
	}

	sendCtx, cancel := context.WithTimeout(ctx, constants.RequestTimeout.BotCommand)
	defer cancel()

	var opts []iris.SendOption

	if threadID, ok := threadIDFromContext(sendCtx); ok {
		opts = append(opts, iris.WithThreadID(threadID))
	}

	if err := t.irisClient.SendMessage(sendCtx, room, message, opts...); err != nil {
		serviceErr := appErrors.NewServiceError("failed to send message", serviceNameIris, "send_message", err)
		return fmt.Errorf("send message to room %s: %w", room, serviceErr)
	}

	return nil
}

func (t *CommandTransport) SendImage(ctx context.Context, room, imageBase64 string) error {
	if t == nil || t.irisClient == nil {
		return errors.New("send image: iris client is not configured")
	}

	sendCtx, cancel := context.WithTimeout(ctx, constants.RequestTimeout.BotCommand)
	defer cancel()

	if err := t.irisClient.SendImage(sendCtx, room, imageBase64); err != nil {
		serviceErr := appErrors.NewServiceError("failed to send image", serviceNameIris, "send_image", err)
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
