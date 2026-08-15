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
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/bot/orchestration/ingress"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/bot/orchestration/transport"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/durability"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/privacylog"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/park285/iris-client-go/webhook"
	sharedlog "github.com/park285/shared-go/pkg/logging"
)

var ErrCommandOutcomeUnknown = errors.New("command execution outcome unknown")

type commandOutcomeUnknownError struct{ cause error }

func (e commandOutcomeUnknownError) Error() string {
	return fmt.Sprintf("%s: %v", ErrCommandOutcomeUnknown, e.cause)
}
func (e commandOutcomeUnknownError) Unwrap() error        { return e.cause }
func (e commandOutcomeUnknownError) Is(target error) bool { return target == ErrCommandOutcomeUnknown }

func IsCommandOutcomeUnknown(err error) bool { return errors.Is(err, ErrCommandOutcomeUnknown) }

func commandOutcome(err error) error {
	if err == nil || IsCommandOutcomeUnknown(err) {
		return err
	}
	if errors.Is(err, transport.ErrReplyStagingFailed) || errors.Is(err, transport.ErrReplyOutcomeUnknown) ||
		errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return commandOutcomeUnknownError{cause: err}
	}
	return err
}

func (b *Bot) ProcessMessage(ctx context.Context, message *webhook.Message) (resultErr error) {
	commandType := "unknown"

	defer func() {
		if r := recover(); r != nil {
			sharedlog.Error(ctx, b.logger, EventBotCommandPanic, "panic in command handler",
				slog.Any("panic", r),
				slog.String("command", commandType),
			)
			resultErr = commandOutcomeUnknownError{cause: fmt.Errorf("process bot command: panic: %v", r)}
		}
	}()

	envelope, ok := b.ensureIngress().Prepare(ctx, message)
	if !ok {
		return nil
	}

	commandType = envelope.CommandType

	cmdCtx := newCommandContextFromIngress(envelope)
	cmdCtx.ThreadID = messageThreadID(message)
	cmdCtx.MessageID = canonicalReplyIdentity(message)
	if cmdCtx.MessageID == "" {
		b.rejectWithoutReplyIdentity(ctx, envelope, commandType)
		return nil
	}

	reqCtx := commandRequestContext(ctx, cmdCtx)
	reqCtx = transport.WithRoomChat(reqCtx, envelope.RoomType, envelope.RoomLinkID)

	if err := b.executeCommand(reqCtx, cmdCtx, envelope.Parsed.Type, envelope.Parsed.Params); err != nil {
		responseErr := b.handleCommandExecutionError(reqCtx, envelope.ChatID, commandType, err)
		if responseErr != nil {
			return commandOutcomeUnknownError{cause: errors.Join(err, responseErr)}
		}
		return commandOutcome(err)
	}
	return nil
}

func (b *Bot) executeCommand(ctx context.Context, cmdCtx *domain.CommandContext, cmdType domain.CommandType, params map[string]any) error {
	return b.ensureCommandExecutor().Execute(ctx, cmdCtx, cmdType, params)
}

func newCommandContextFromIngress(envelope *ingress.Envelope) *domain.CommandContext {
	return domain.NewCommandContext(
		envelope.ChatID,
		envelope.RoomName,
		envelope.UserID,
		envelope.UserName,
		envelope.Parsed.RawMessage,
		false,
	)
}

func messageThreadID(message *webhook.Message) *string {
	if message == nil || message.JSON == nil || message.JSON.ThreadID == nil {
		return nil
	}

	trimmed := strings.TrimSpace(*message.JSON.ThreadID)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func commandRequestContext(ctx context.Context, cmdCtx *domain.CommandContext) context.Context {
	ctx = transport.WithReplyIdentity(ctx, cmdCtx.MessageID)
	if cmdCtx.ThreadID != nil {
		ctx = transport.WithThreadID(ctx, *cmdCtx.ThreadID)
	}
	return ctx
}

// Iris는 blank message id로 payload를 만들지 않고(WebhookPayloadError::BlankMessageId),
// webhook SDK도 X-Iris-Message-Id 없는 요청을 400으로 막는다. 그래도 식별자 없이 도달하면
// 재전달마다 다른 응답 id가 생겨 중복 발화가 되므로 폴백 없이 빈 값을 돌려준다.
func canonicalReplyIdentity(message *webhook.Message) string {
	if message == nil || message.JSON == nil {
		return ""
	}

	return durability.MessageIdentity(message.JSON.MessageID)
}

func (b *Bot) rejectWithoutReplyIdentity(ctx context.Context, envelope *ingress.Envelope, commandType string) {
	sharedlog.Warn(ctx, b.logger, EventBotReplyIdentityMissing,
		"refusing command without canonical message id",
		privacylog.ChatAttr(envelope.ChatID, envelope.RoomName),
		slog.String("command", commandType),
	)
}

func (b *Bot) handleCommandExecutionError(ctx context.Context, chatID, commandType string, err error) error {
	errorMsg := b.getErrorMessage(err)
	if chatID == "" {
		return nil
	}
	if b.skipErrorResponseOnUnknownOutcome(ctx, chatID, commandType, err) {
		return nil
	}
	if sendErr := b.sendError(ctx, chatID, errorMsg); sendErr != nil {
		errorAttrs := sharedlog.ErrorAttrs(sendErr)
		attrs := make([]slog.Attr, 0, 2+len(errorAttrs))
		attrs = append(attrs,
			privacylog.ChatIDAttr(chatID),
			slog.String("command", commandType),
		)
		attrs = append(attrs, errorAttrs...)
		sharedlog.Error(ctx, b.logger, EventBotCommandErrorResponseFailed, "failed to send command error response", attrs...)
		return sendErr
	}
	return nil
}
