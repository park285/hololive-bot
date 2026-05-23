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

package orchestration

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/kapu/hololive-kakao-bot-go/internal/bot/internal/orchestration/orchcmd"
	"github.com/kapu/hololive-shared/pkg/domain"
	sharedlog "github.com/park285/hololive-bot/shared-go/pkg/logging"
	"github.com/park285/iris-client-go/iris"
)

// HTTP webhook 핸들러에서 호출하기 위해 public으로 노출됩니다.
func (b *Bot) HandleMessage(ctx context.Context, message *iris.Message) {
	commandType := "unknown"

	defer func() {
		if r := recover(); r != nil {
			sharedlog.Error(ctx, b.logger, EventBotCommandPanic, "panic in command handler",
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

	cmdCtx := newCommandContextFromIngress(envelope)
	cmdCtx.ThreadID = messageThreadID(message)
	reqCtx := commandRequestContext(ctx, cmdCtx, message)

	if orchcmd.ShouldExecuteAsync(envelope.Parsed.Type) {
		b.executeCommandAsync(reqCtx, cmdCtx, envelope.Parsed.Type, envelope.Parsed.Params, commandType, envelope.ChatID)
		return
	}

	if err := b.executeCommand(reqCtx, cmdCtx, envelope.Parsed.Type, envelope.Parsed.Params); err != nil {
		b.handleCommandExecutionError(reqCtx, envelope.ChatID, commandType, err)
	}
}

func newCommandContextFromIngress(envelope *ingressEnvelope) *domain.CommandContext {
	return domain.NewCommandContext(
		envelope.ChatID,
		envelope.RoomName,
		envelope.UserID,
		envelope.UserName,
		envelope.Parsed.RawMessage,
		false,
	)
}

func messageThreadID(message *iris.Message) *string {
	if message == nil || message.JSON == nil || message.JSON.ThreadID == nil {
		return nil
	}

	trimmed := strings.TrimSpace(*message.JSON.ThreadID)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func commandRequestContext(ctx context.Context, cmdCtx *domain.CommandContext, message *iris.Message) context.Context {
	if identity := messageReplyIdentity(message); identity != "" {
		ctx = withReplyIdentity(ctx, identity)
	}
	if cmdCtx.ThreadID != nil {
		ctx = withThreadID(ctx, *cmdCtx.ThreadID)
	}
	return ctx
}

func messageReplyIdentity(message *iris.Message) string {
	if message == nil || message.JSON == nil {
		return ""
	}
	if trimmed := strings.TrimSpace(message.JSON.MessageID); trimmed != "" {
		return "message:" + trimmed
	}
	if trimmed := strings.TrimSpace(message.JSON.ChatLogID); trimmed != "" {
		return "chat-log:" + trimmed
	}
	if message.JSON.SourceLogID != nil {
		return fmt.Sprintf("source-log:%d", *message.JSON.SourceLogID)
	}
	return ""
}

func (b *Bot) handleCommandExecutionError(ctx context.Context, chatID, commandType string, err error) {
	errorMsg := b.getErrorMessage(err, commandType)
	if chatID == "" {
		return
	}
	if sendErr := b.sendError(ctx, chatID, errorMsg); sendErr != nil {
		attrs := []slog.Attr{
			slog.String("chat_id", chatID),
			slog.String("command", commandType),
		}
		attrs = append(attrs, sharedlog.ErrorAttrs(sendErr)...)
		sharedlog.Error(ctx, b.logger, EventBotCommandErrorResponseFailed, "failed to send command error response", attrs...)
	}
}
