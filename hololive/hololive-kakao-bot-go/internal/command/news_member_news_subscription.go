package command

import (
	"context"
	"fmt"

	"github.com/kapu/hololive-shared/pkg/domain"

	"github.com/kapu/hololive-kakao-bot-go/internal/adapter"
)

// MemberNewsSubscriptionCommand: 뉴스 알림 구독 제어 명령어.
type MemberNewsSubscriptionCommand struct {
	BaseCommand
}

// NewMemberNewsSubscriptionCommand: 명령어 생성.
func NewMemberNewsSubscriptionCommand(deps *Dependencies) *MemberNewsSubscriptionCommand {
	return &MemberNewsSubscriptionCommand{BaseCommand: NewBaseCommand(deps)}
}

func (c *MemberNewsSubscriptionCommand) Name() string {
	return "news_subscription"
}

func (c *MemberNewsSubscriptionCommand) Description() string {
	return "뉴스 알림 구독 제어"
}

func (c *MemberNewsSubscriptionCommand) Execute(ctx context.Context, cmdCtx *domain.CommandContext, params map[string]any) error {
	if err := c.EnsureBaseDeps(); err != nil {
		return fmt.Errorf("ensure base deps: %w", err)
	}

	if c.Deps().MemberNews == nil {
		return c.Deps().SendError(ctx, cmdCtx.Room, adapter.ErrMemberNewsServiceNotInitialized)
	}

	action := "status"
	if rawAction, ok := params["action"].(string); ok && rawAction != "" {
		action = rawAction
	}

	switch action {
	case "on", "켜기", "구독":
		return c.handleSubscribe(ctx, cmdCtx)
	case "off", "끄기", "해제":
		return c.handleUnsubscribe(ctx, cmdCtx)
	default:
		return c.handleStatus(ctx, cmdCtx)
	}
}

func (c *MemberNewsSubscriptionCommand) handleSubscribe(ctx context.Context, cmdCtx *domain.CommandContext) error {
	isSubscribed, err := c.Deps().MemberNews.IsRoomSubscribed(ctx, cmdCtx.Room)
	if err != nil {
		return c.Deps().SendError(ctx, cmdCtx.Room, adapter.ErrMemberNewsSubscriptionFailed)
	}
	if isSubscribed {
		return c.Deps().SendMessage(ctx, cmdCtx.Room, c.Deps().Formatter.FormatMemberNewsAlreadySubscribed(ctx))
	}

	if err := c.Deps().MemberNews.SubscribeRoom(ctx, cmdCtx.Room, cmdCtx.RoomName); err != nil {
		c.Deps().Logger.Error("Member news subscribe failed", "room", cmdCtx.Room, "error", err)
		return c.Deps().SendError(ctx, cmdCtx.Room, adapter.ErrMemberNewsSubscriptionFailed)
	}

	return c.Deps().SendMessage(ctx, cmdCtx.Room, c.Deps().Formatter.FormatMemberNewsSubscribed(ctx))
}

func (c *MemberNewsSubscriptionCommand) handleUnsubscribe(ctx context.Context, cmdCtx *domain.CommandContext) error {
	isSubscribed, err := c.Deps().MemberNews.IsRoomSubscribed(ctx, cmdCtx.Room)
	if err != nil {
		return c.Deps().SendError(ctx, cmdCtx.Room, adapter.ErrMemberNewsSubscriptionFailed)
	}
	if !isSubscribed {
		return c.Deps().SendMessage(ctx, cmdCtx.Room, c.Deps().Formatter.FormatMemberNewsNotSubscribed(ctx))
	}

	if err := c.Deps().MemberNews.UnsubscribeRoom(ctx, cmdCtx.Room); err != nil {
		c.Deps().Logger.Error("Member news unsubscribe failed", "room", cmdCtx.Room, "error", err)
		return c.Deps().SendError(ctx, cmdCtx.Room, adapter.ErrMemberNewsSubscriptionFailed)
	}

	return c.Deps().SendMessage(ctx, cmdCtx.Room, c.Deps().Formatter.FormatMemberNewsUnsubscribed(ctx))
}

func (c *MemberNewsSubscriptionCommand) handleStatus(ctx context.Context, cmdCtx *domain.CommandContext) error {
	isSubscribed, err := c.Deps().MemberNews.IsRoomSubscribed(ctx, cmdCtx.Room)
	if err != nil {
		return c.Deps().SendError(ctx, cmdCtx.Room, adapter.ErrMemberNewsSubscriptionFailed)
	}
	return c.Deps().SendMessage(ctx, cmdCtx.Room, c.Deps().Formatter.FormatMemberNewsStatus(ctx, isSubscribed))
}
