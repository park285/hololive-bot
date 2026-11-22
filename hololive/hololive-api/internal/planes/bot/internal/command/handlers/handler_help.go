package handlers

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/bot/orchestration/transport"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/botcommand"
	handlercore "github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers/handlercore"
)

type HelpCommand struct {
	deps *handlercore.Dependencies
}

func NewHelpCommand(deps *handlercore.Dependencies) *HelpCommand {
	return &HelpCommand{deps: deps}
}

func (c *HelpCommand) Name() string {
	return "help"
}

func (c *HelpCommand) Description() string {
	return "도움말을 표시합니다"
}

func (c *HelpCommand) Execute(ctx context.Context, cmdCtx *botcommand.CommandContext, _ botcommand.NoParams) error {
	if cmdCtx == nil {
		return errors.New("help command context is nil")
	}

	content, contentErr := c.deps.Formatter.FormatHelpContent(ctx)
	if contentErr != nil {
		return fmt.Errorf("format help content: %w", contentErr)
	}

	images, loadErr := c.loadHelpImages(ctx)
	if loadErr != nil {
		return c.sendTextFallback(ctx, cmdCtx.Room, content.TextFallback, imageTextFallbackReasonRenderFailed, loadErr)
	}

	sendErr := c.deps.SendImages(ctx, cmdCtx.Room, images)
	if sendErr == nil {
		return nil
	}

	sendErr = fmt.Errorf("send help image album: %w", sendErr)

	if transport.IsReplyOutcomeUnknown(sendErr) {
		observeImageTextFallback(c.Name(), imageTextFallbackReasonOutcomeUnknown)
		c.logImageFallback(ctx, sendErr)

		return fmt.Errorf("send help images: %w", sendErr)
	}

	return c.sendTextFallback(ctx, cmdCtx.Room, content.TextFallback, imageTextFallbackReasonSendFailed, sendErr)
}

func (c *HelpCommand) loadHelpImages(ctx context.Context) ([][]byte, error) {
	images, err := c.deps.HelpImageProvider.HelpImages(ctx)
	if err != nil {
		return nil, fmt.Errorf("load help images: %w", err)
	}

	if len(images) == 0 {
		return nil, errors.New("load help images: empty result")
	}

	for index, imageData := range images {
		if len(imageData) == 0 {
			return nil, fmt.Errorf("load help image %d/%d: empty payload", index+1, len(images))
		}
	}

	return images, nil
}

func (c *HelpCommand) sendTextFallback(ctx context.Context, room, text, reason string, imageErr error) error {
	observeImageTextFallback(c.Name(), reason)
	c.logImageFallback(ctx, imageErr)

	if err := c.deps.SendMessage(ctx, room, text); err != nil {
		return errors.Join(imageErr, fmt.Errorf("send help text fallback: %w", err))
	}

	return nil
}

func (c *HelpCommand) logImageFallback(ctx context.Context, err error) {
	if c.deps.Logger == nil {
		return
	}

	c.deps.Logger.WarnContext(ctx, "help_image_fallback", slog.Any("error", err))
}

func (c *HelpCommand) CheckDependencies() error {
	if c == nil || c.deps == nil {
		return errors.New("help command dependencies not configured")
	}

	if c.deps.SendMessage == nil {
		return errors.New("message callback not configured")
	}

	if c.deps.Formatter == nil {
		return errors.New("formatter not configured")
	}

	if c.deps.HelpImageProvider == nil || c.deps.SendImages == nil {
		return errors.New("help image capability not configured")
	}

	return nil
}
