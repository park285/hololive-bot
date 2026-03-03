package command

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

func NewMajorEventCommand(deps *Dependencies, repo MajorEventRepository) *MajorEventCommand {
	return &MajorEventCommand{
		BaseCommand: NewBaseCommand(deps),
		repository:  repo,
	}
}

func (c *MajorEventCommand) Name() string {
	return "major_event"
}

func (c *MajorEventCommand) Description() string {
	return "행사 알림 관리"
}

func (c *MajorEventCommand) Execute(ctx context.Context, cmdCtx *domain.CommandContext, params map[string]any) error {
	if err := c.EnsureBaseDeps(); err != nil {
		return fmt.Errorf("failed to ensure base dependencies: %w", err)
	}

	if c.repository == nil {
		return c.Deps().SendError(ctx, cmdCtx.Room, "행사 알림 서비스가 초기화되지 않았습니다")
	}

	action, hasAction := params["action"].(string)
	if !hasAction {
		action = "status"
	}

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
