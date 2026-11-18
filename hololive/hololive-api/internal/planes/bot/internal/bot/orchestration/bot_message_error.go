package orchestration

import (
	"context"
	"fmt"
	"log/slog"

	sharedlog "github.com/park285/shared-go/v2/pkg/logging"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/bot/orchestration/transport"
	sharedprivacylog "github.com/kapu/hololive-shared/pkg/privacylog"
)

func (b *Bot) sendError(ctx context.Context, room, errorMsg string) error {
	if err := b.transport.SendError(ctx, room, errorMsg); err != nil {
		return fmt.Errorf("send error reply: %w", err)
	}

	return nil
}

func (b *Bot) sendMessage(ctx context.Context, room, message string) error {
	if err := b.transport.SendMessage(ctx, room, message); err != nil {
		return fmt.Errorf("send message: %w", err)
	}

	return nil
}

func (b *Bot) sendImage(ctx context.Context, room string, imageData []byte) error {
	if err := b.transport.SendImage(ctx, room, imageData); err != nil {
		return fmt.Errorf("send image: %w", err)
	}

	return nil
}

func (b *Bot) sendImages(ctx context.Context, room string, images [][]byte) error {
	if err := b.transport.SendImages(ctx, room, images); err != nil {
		return fmt.Errorf("send images: %w", err)
	}

	return nil
}

func (b *Bot) skipErrorResponseOnUnknownOutcome(ctx context.Context, chatID, commandType string, err error) bool {
	if !transport.IsReplyOutcomeUnknown(err) {
		return false
	}

	errorAttrs := sharedlog.ErrorAttrs(err)
	attrs := make([]slog.Attr, 0, 2+len(errorAttrs))

	attrs = append(attrs,
		sharedprivacylog.ChatIDAttr(chatID),
		slog.String("command", commandType),
	)
	attrs = append(attrs, errorAttrs...)
	sharedlog.Warn(ctx, b.logger, EventBotReplyOutcomeUnknown, "iris reply outcome unknown; suppressing error response", attrs...)

	return true
}
