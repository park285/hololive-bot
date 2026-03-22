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
	"log/slog"
	"strings"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/iris"
)

// HandleMessage: Iris webhook으로부터 수신한 메시지를 처리합니다.
// HTTP webhook 핸들러에서 호출하기 위해 public으로 노출됩니다.
func (b *Bot) HandleMessage(ctx context.Context, message *iris.Message) {
	commandType := "unknown"

	defer func() {
		if r := recover(); r != nil {
			b.logger.Error("Panic in handleMessage",
				slog.Any("panic", r),
				slog.String("command", commandType),
			)
		}
	}()

	envelope, ok := b.ensureIngress().Prepare(message)
	if !ok {
		return
	}

	commandType = envelope.CommandType

	cmdCtx := domain.NewCommandContext(
		envelope.ChatID,
		envelope.RoomName,
		envelope.UserID,
		envelope.UserName,
		envelope.Parsed.RawMessage,
		false,
	)

	if message != nil && message.JSON != nil && message.JSON.ThreadID != nil {
		if trimmed := strings.TrimSpace(*message.JSON.ThreadID); trimmed != "" {
			cmdCtx.ThreadID = &trimmed
		}
	}

	reqCtx := ctx

	if cmdCtx.ThreadID != nil {
		reqCtx = withThreadID(ctx, *cmdCtx.ThreadID)
	}

	if shouldExecuteAsync(envelope.Parsed.Type) {
		b.executeCommandAsync(reqCtx, cmdCtx, envelope.Parsed.Type, envelope.Parsed.Params, commandType, envelope.ChatID)
		return
	}

	if err := b.executeCommand(reqCtx, cmdCtx, envelope.Parsed.Type, envelope.Parsed.Params); err != nil {
		b.logger.Error("Failed to execute command", slog.Any("error", err))

		errorMsg := b.getErrorMessage(err, commandType)

		if envelope.ChatID != "" {
			if sendErr := b.sendError(reqCtx, envelope.ChatID, errorMsg); sendErr != nil {
				b.logger.Error("Failed to send command error message", slog.Any("error", sendErr), slog.String("chat_id", envelope.ChatID))
			}
		}
	}
}
