package orchestration

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/park285/iris-client-go/v5/webhook"
	sharedlog "github.com/park285/shared-go/v2/pkg/logging"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/bot/orchestration/ingress"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/bot/orchestration/transport"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/botcommand"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/durability"
	sharedprivacylog "github.com/kapu/hololive-shared/pkg/privacylog"
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

	if transport.IsReplyOutcomeUnknown(err) ||
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
				slog.Group("panic", slog.String("type", fmt.Sprintf("%T", r))),
				slog.String("command", commandType),
			)

			resultErr = commandOutcomeUnknownError{cause: fmt.Errorf("process bot command: panic type: %T", r)}
		}
	}()

	envelope, ok := b.ingress.Prepare(ctx, message)
	if !ok {
		return nil
	}

	commandType = envelope.CommandType

	cmdCtx := newCommandContextFromIngress(envelope)

	cmdCtx.ThreadID = messageThreadID(message)
	cmdCtx.MessageID = canonicalReplyIdentity(message)

	reqCtx := commandRequestContext(ctx, cmdCtx)

	reqCtx = transport.WithRoomChat(reqCtx, envelope.RoomType, envelope.RoomLinkID)
	reqCtx = b.transport.WithReplyFormat(reqCtx, envelope.ChatID)

	if err := b.commandExecutor.Execute(reqCtx, cmdCtx, envelope.Parsed.Type, envelope.Parsed.Params); err != nil {
		responseErr := b.handleCommandExecutionError(reqCtx, envelope.ChatID, commandType, err)
		if responseErr != nil {
			return commandOutcomeUnknownError{cause: errors.Join(err, responseErr)}
		}

		return commandOutcome(err)
	}

	return nil
}

func (b *Bot) AcceptsMessage(ctx context.Context, message *webhook.Message) bool {
	return b.ingress.Accepts(ctx, message)
}

func newCommandContextFromIngress(envelope *ingress.Envelope) *botcommand.CommandContext {
	return botcommand.NewCommandContext(
		envelope.ChatID,
		envelope.UserID,
		envelope.UserName,
		envelope.Parsed.RawMessage,
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

func commandRequestContext(ctx context.Context, cmdCtx *botcommand.CommandContext) context.Context {
	ctx = transport.WithReplyIdentity(ctx, cmdCtx.MessageID)
	if cmdCtx.ThreadID != nil {
		ctx = transport.WithThreadID(ctx, *cmdCtx.ThreadID)
	}

	return ctx
}

func canonicalReplyIdentity(message *webhook.Message) string {
	if message == nil || message.JSON == nil {
		return ""
	}

	return durability.MessageIdentity(message.JSON.MessageID)
}

func (b *Bot) handleCommandExecutionError(ctx context.Context, chatID, commandType string, err error) error {
	if chatID == "" {
		return nil
	}

	if b.skipErrorResponseOnUnknownOutcome(ctx, chatID, commandType, err) {
		return nil
	}

	if sendErr := b.sendError(ctx, chatID, messaging.ErrCommandProcessingFailed); sendErr != nil {
		errorAttrs := sharedlog.ErrorAttrs(sendErr)
		attrs := make([]slog.Attr, 0, 2+len(errorAttrs))

		attrs = append(attrs,
			sharedprivacylog.ChatIDAttr(chatID),
			slog.String("command", commandType),
		)
		attrs = append(attrs, errorAttrs...)
		sharedlog.Error(ctx, b.logger, EventBotCommandErrorResponseFailed, "failed to send command error response", attrs...)

		return sendErr
	}

	return nil
}
