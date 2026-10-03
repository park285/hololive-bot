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

package handlers

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/bot/orchestration/transport"
	handlercore "github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers/handlercore"
	"github.com/kapu/hololive-shared/pkg/domain"
)

var errHelpImageUnavailable = errors.New("help image capability is unavailable")

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

func (c *HelpCommand) Execute(ctx context.Context, cmdCtx *domain.CommandContext, _ map[string]any) error {
	if c == nil {
		return errors.New("help command dependencies not configured")
	}

	if cmdCtx == nil {
		return errors.New("help command context is nil")
	}

	if err := c.ensureDeps(); err != nil {
		return fmt.Errorf("failed to ensure dependencies: %w", err)
	}

	// 도움말 내용을 만들지 못하면 코드 대체 문구를 보내지 않고 오류를 돌려준다. bot의 공통 오류 경로가
	// message_strings의 명령 실패 문구로 응답한다(DEC-20260926-hololive-message-strings-startup-validation).
	content, contentErr := c.deps.Formatter.FormatHelpContent(ctx)
	if contentErr != nil {
		return fmt.Errorf("format help content: %w", contentErr)
	}

	imageErr := c.sendHelpImages(ctx, cmdCtx.Room)
	if imageErr == nil {
		return nil
	}

	c.logImageFallback(ctx, imageErr)

	if transport.IsReplyOutcomeUnknown(imageErr) {
		return fmt.Errorf("send help images: %w", imageErr)
	}

	if err := c.deps.SendMessage(ctx, cmdCtx.Room, content.TextFallback); err != nil {
		return errors.Join(imageErr, fmt.Errorf("send help text fallback: %w", err))
	}

	return nil
}

func (c *HelpCommand) sendHelpImages(ctx context.Context, room string) error {
	if c.deps.HelpImageProvider == nil || c.deps.SendImages == nil {
		return errHelpImageUnavailable
	}

	images, err := c.deps.HelpImageProvider.HelpImages(ctx)
	if err != nil {
		return fmt.Errorf("load help images: %w", err)
	}

	if len(images) == 0 {
		return errors.New("load help images: empty result")
	}

	if err := c.sendHelpImagePayloads(ctx, room, images); err != nil {
		return fmt.Errorf("send help image payloads: %w", err)
	}

	return nil
}

func (c *HelpCommand) sendHelpImagePayloads(ctx context.Context, room string, images [][]byte) error {
	for index, imageData := range images {
		if len(imageData) == 0 {
			return fmt.Errorf("load help image %d/%d: empty payload", index+1, len(images))
		}
	}

	if err := c.deps.SendImages(ctx, room, images); err != nil {
		return fmt.Errorf("send help image album: %w", err)
	}

	return nil
}

func (c *HelpCommand) logImageFallback(ctx context.Context, err error) {
	if c.deps.Logger == nil {
		return
	}

	c.deps.Logger.WarnContext(ctx, "help_image_fallback", slog.Any("error", err))
}

func (c *HelpCommand) ensureDeps() error {
	if c == nil || c.deps == nil {
		return errors.New("help command dependencies not configured")
	}

	if c.deps.SendMessage == nil {
		return errors.New("message callback not configured")
	}

	if c.deps.Formatter == nil {
		return errors.New("formatter not configured")
	}

	return nil
}
