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
	"fmt"
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/domain"
)

type MajorEventCommand struct {
	BaseCommand

	repository MajorEventRepository
}

func NewMajorEventCommand(deps *Dependencies, repository MajorEventRepository) *MajorEventCommand {
	return &MajorEventCommand{
		BaseCommand: NewBaseCommand(deps),
		repository:  repository,
	}
}

func (c *MajorEventCommand) Name() string {
	return "major_event"
}

func (c *MajorEventCommand) Description() string {
	return "행사 알림 관리"
}

func (c *MajorEventCommand) Execute(ctx context.Context, cmdCtx *domain.CommandContext, params map[string]any) error {
	if err := c.ensureMajorEventReady(ctx, cmdCtx); err != nil {
		return err
	}

	return c.dispatchMajorEventAction(ctx, cmdCtx, majorEventAction(params))
}

func (c *MajorEventCommand) ensureMajorEventReady(ctx context.Context, cmdCtx *domain.CommandContext) error {
	if err := c.EnsureBaseDeps(); err != nil {
		return fmt.Errorf("failed to ensure base dependencies: %w", err)
	}

	if c.repository == nil {
		return c.Deps().SendError(ctx, cmdCtx.Room, "행사 알림 서비스가 초기화되지 않았습니다")
	}

	return nil
}

func majorEventAction(params map[string]any) string {
	action, hasAction := params["action"].(string)
	if !hasAction {
		return "status"
	}

	return action
}

func (c *MajorEventCommand) dispatchMajorEventAction(ctx context.Context, cmdCtx *domain.CommandContext, action string) error {
	switch action {
	case "on", "켜기":
		return c.handleSubscribe(ctx, cmdCtx)
	case "off", "끄기":
		return c.handleUnsubscribe(ctx, cmdCtx)
	case "list", "목록", "status":
		return c.handleStatus(ctx, cmdCtx)
	default:
		return c.Deps().SendMessage(ctx, cmdCtx.Room, c.Deps().Formatter.FormatMajorEventUsage(ctx))
	}
}

func (c *MajorEventCommand) handleSubscribe(ctx context.Context, cmdCtx *domain.CommandContext) error {
	isSubscribed, err := c.repository.IsSubscribed(ctx, cmdCtx.Room)
	if err != nil {
		c.Deps().Logger.Error("Failed to check subscription", slog.String("error", err.Error()))
		return c.Deps().SendError(ctx, cmdCtx.Room, "구독 상태 확인 중 오류가 발생했습니다")
	}

	if isSubscribed {
		return c.Deps().SendMessage(ctx, cmdCtx.Room, c.Deps().Formatter.FormatMajorEventAlreadySubscribed(ctx))
	}

	if err := c.repository.Subscribe(ctx, cmdCtx.Room, cmdCtx.RoomName); err != nil {
		c.Deps().Logger.Error("Failed to subscribe", slog.String("error", err.Error()))
		return c.Deps().SendError(ctx, cmdCtx.Room, "구독 중 오류가 발생했습니다")
	}

	return c.Deps().SendMessage(ctx, cmdCtx.Room, c.Deps().Formatter.FormatMajorEventSubscribed(ctx))
}

func (c *MajorEventCommand) handleUnsubscribe(ctx context.Context, cmdCtx *domain.CommandContext) error {
	isSubscribed, err := c.repository.IsSubscribed(ctx, cmdCtx.Room)
	if err != nil {
		c.Deps().Logger.Error("Failed to check subscription", slog.String("error", err.Error()))
		return c.Deps().SendError(ctx, cmdCtx.Room, "구독 상태 확인 중 오류가 발생했습니다")
	}

	if !isSubscribed {
		return c.Deps().SendMessage(ctx, cmdCtx.Room, c.Deps().Formatter.FormatMajorEventNotSubscribed(ctx))
	}

	if err := c.repository.Unsubscribe(ctx, cmdCtx.Room); err != nil {
		c.Deps().Logger.Error("Failed to unsubscribe", slog.String("error", err.Error()))
		return c.Deps().SendError(ctx, cmdCtx.Room, "구독 해제 중 오류가 발생했습니다")
	}

	return c.Deps().SendMessage(ctx, cmdCtx.Room, c.Deps().Formatter.FormatMajorEventUnsubscribed(ctx))
}

func (c *MajorEventCommand) handleStatus(ctx context.Context, cmdCtx *domain.CommandContext) error {
	isSubscribed, err := c.repository.IsSubscribed(ctx, cmdCtx.Room)
	if err != nil {
		c.Deps().Logger.Error("Failed to check subscription", slog.String("error", err.Error()))
		return c.Deps().SendError(ctx, cmdCtx.Room, "구독 상태 확인 중 오류가 발생했습니다")
	}

	return c.Deps().SendMessage(ctx, cmdCtx.Room, c.Deps().Formatter.FormatMajorEventStatus(ctx, isSubscribed))
}
