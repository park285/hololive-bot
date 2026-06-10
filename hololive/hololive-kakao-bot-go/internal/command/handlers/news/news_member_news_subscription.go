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

package news

import (
	"context"
	"fmt"

	"github.com/kapu/hololive-shared/pkg/domain"

	"github.com/kapu/hololive-kakao-bot-go/internal/adapter"
	"github.com/kapu/hololive-kakao-bot-go/internal/command/handlers/handlercore"
)

type MemberNewsSubscriptionCommand struct {
	handlercore.BaseCommand
}

func NewMemberNewsSubscriptionCommand(deps *handlercore.Dependencies) *MemberNewsSubscriptionCommand {
	return &MemberNewsSubscriptionCommand{BaseCommand: handlercore.NewBaseCommand(deps)}
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

	switch memberNewsSubscriptionAction(params) {
	case "on", "켜기", "구독":
		return c.handleSubscribe(ctx, cmdCtx)
	case "off", "끄기", "해제":
		return c.handleUnsubscribe(ctx, cmdCtx)
	default:
		return c.handleStatus(ctx, cmdCtx)
	}
}

func memberNewsSubscriptionAction(params map[string]any) string {
	rawAction, ok := params["action"].(string)
	if !ok || rawAction == "" {
		return "status"
	}

	return rawAction
}

type memberNewsSubscriptionPort struct {
	service handlercore.MemberNewsService
}

func (p memberNewsSubscriptionPort) IsSubscribed(ctx context.Context, roomID string) (bool, error) {
	return p.service.IsRoomSubscribed(ctx, roomID)
}

func (p memberNewsSubscriptionPort) Subscribe(ctx context.Context, roomID, roomName string) error {
	return p.service.SubscribeRoom(ctx, roomID, roomName)
}

func (p memberNewsSubscriptionPort) Unsubscribe(ctx context.Context, roomID string) error {
	return p.service.UnsubscribeRoom(ctx, roomID)
}

func (c *MemberNewsSubscriptionCommand) subscriptionFlow(cmdCtx *domain.CommandContext) handlercore.SubscriptionFlow {
	return handlercore.NewSubscriptionFlow(handlercore.SubscriptionFlowConfig{
		Port: memberNewsSubscriptionPort{service: c.Deps().MemberNews},
		OnCheckError: func(ctx context.Context, _ error) error {
			return c.Deps().SendError(ctx, cmdCtx.Room, adapter.ErrMemberNewsSubscriptionFailed)
		},
		OnAlreadySubscribed: func(ctx context.Context) error {
			return c.Deps().SendMessage(ctx, cmdCtx.Room, c.Deps().Formatter.FormatMemberNewsAlreadySubscribed(ctx))
		},
		OnSubscribeError: func(ctx context.Context, err error) error {
			c.Deps().Logger.Error("Member news subscribe failed", "room", cmdCtx.Room, "error", err)
			return c.Deps().SendError(ctx, cmdCtx.Room, adapter.ErrMemberNewsSubscriptionFailed)
		},
		OnSubscribed: func(ctx context.Context) error {
			return c.Deps().SendMessage(ctx, cmdCtx.Room, c.Deps().Formatter.FormatMemberNewsSubscribed(ctx))
		},
		OnNotSubscribed: func(ctx context.Context) error {
			return c.Deps().SendMessage(ctx, cmdCtx.Room, c.Deps().Formatter.FormatMemberNewsNotSubscribed(ctx))
		},
		OnUnsubscribeError: func(ctx context.Context, err error) error {
			c.Deps().Logger.Error("Member news unsubscribe failed", "room", cmdCtx.Room, "error", err)
			return c.Deps().SendError(ctx, cmdCtx.Room, adapter.ErrMemberNewsSubscriptionFailed)
		},
		OnUnsubscribed: func(ctx context.Context) error {
			return c.Deps().SendMessage(ctx, cmdCtx.Room, c.Deps().Formatter.FormatMemberNewsUnsubscribed(ctx))
		},
		OnStatus: func(ctx context.Context, subscribed bool) error {
			return c.Deps().SendMessage(ctx, cmdCtx.Room, c.Deps().Formatter.FormatMemberNewsStatus(ctx, subscribed))
		},
	})
}

func (c *MemberNewsSubscriptionCommand) handleSubscribe(ctx context.Context, cmdCtx *domain.CommandContext) error {
	return c.subscriptionFlow(cmdCtx).Subscribe(ctx, cmdCtx.Room, cmdCtx.RoomName)
}

func (c *MemberNewsSubscriptionCommand) handleUnsubscribe(ctx context.Context, cmdCtx *domain.CommandContext) error {
	return c.subscriptionFlow(cmdCtx).Unsubscribe(ctx, cmdCtx.Room)
}

func (c *MemberNewsSubscriptionCommand) handleStatus(ctx context.Context, cmdCtx *domain.CommandContext) error {
	return c.subscriptionFlow(cmdCtx).Status(ctx, cmdCtx.Room)
}
