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

	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers/handlercore"
	"github.com/kapu/hololive-shared/pkg/domain"
)

type MajorEventCommand struct {
	handlercore.BaseCommand

	repository handlercore.MajorEventRepository
}

func NewMajorEventCommand(deps *handlercore.Dependencies, repository handlercore.MajorEventRepository) *MajorEventCommand {
	return &MajorEventCommand{
		BaseCommand: handlercore.NewBaseCommand(deps),
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
		return fmt.Errorf("ensure major event ready: %w", err)
	}

	if err := c.dispatchMajorEventAction(ctx, cmdCtx, majorEventAction(params)); err != nil {
		return fmt.Errorf("dispatch major event action: %w", err)
	}

	return nil
}

func (c *MajorEventCommand) ensureMajorEventReady(ctx context.Context, cmdCtx *domain.CommandContext) error {
	if err := c.EnsureBaseDeps(); err != nil {
		return fmt.Errorf("failed to ensure base dependencies: %w", err)
	}

	if c.repository == nil {
		if err := c.Deps().SendError(ctx, cmdCtx.Room, messaging.ErrMajorEventServiceNotInitialized); err != nil {
			return fmt.Errorf("send error: %w", err)
		}

		return nil
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
	handler := c.majorEventActionHandler(action)
	if err := handler(ctx, cmdCtx); err != nil {
		return fmt.Errorf("handle major event action: %w", err)
	}

	return nil
}

func (c *MajorEventCommand) majorEventActionHandler(action string) func(context.Context, *domain.CommandContext) error {
	handlers := map[string]func(context.Context, *domain.CommandContext) error{
		"on":     c.handleSubscribe,
		"켜기":     c.handleSubscribe,
		"off":    c.handleUnsubscribe,
		"끄기":     c.handleUnsubscribe,
		"list":   c.handleStatus,
		"목록":     c.handleStatus,
		"status": c.handleStatus,
	}
	if handler := handlers[action]; handler != nil {
		return handler
	}

	return c.handleUsage
}

func (c *MajorEventCommand) handleUsage(ctx context.Context, cmdCtx *domain.CommandContext) error {
	if err := c.Deps().SendMessage(ctx, cmdCtx.Room, c.Deps().Formatter.FormatMajorEventUsage(ctx)); err != nil {
		return fmt.Errorf("send message: %w", err)
	}

	return nil
}

func (c *MajorEventCommand) subscriptionFlow(cmdCtx *domain.CommandContext) handlercore.SubscriptionFlow {
	return handlercore.NewSubscriptionFlow(&handlercore.SubscriptionFlowConfig{
		Port: c.repository,
		OnCheckError: func(ctx context.Context, err error) error {
			c.Deps().Logger.Error("Failed to check subscription", slog.String("error", err.Error()))

			return c.Deps().SendError(ctx, cmdCtx.Room, messaging.ErrMajorEventStatusCheckFailed)
		},
		OnAlreadySubscribed: func(ctx context.Context) error {
			return c.Deps().SendMessage(ctx, cmdCtx.Room, c.Deps().Formatter.FormatMajorEventAlreadySubscribed(ctx))
		},
		OnSubscribeError: func(ctx context.Context, err error) error {
			c.Deps().Logger.Error("Failed to subscribe", slog.String("error", err.Error()))

			return c.Deps().SendError(ctx, cmdCtx.Room, messaging.ErrMajorEventSubscribeFailed)
		},
		OnSubscribed: func(ctx context.Context) error {
			return c.Deps().SendMessage(ctx, cmdCtx.Room, c.Deps().Formatter.FormatMajorEventSubscribed(ctx))
		},
		OnNotSubscribed: func(ctx context.Context) error {
			return c.Deps().SendMessage(ctx, cmdCtx.Room, c.Deps().Formatter.FormatMajorEventNotSubscribed(ctx))
		},
		OnUnsubscribeError: func(ctx context.Context, err error) error {
			c.Deps().Logger.Error("Failed to unsubscribe", slog.String("error", err.Error()))

			return c.Deps().SendError(ctx, cmdCtx.Room, messaging.ErrMajorEventUnsubscribeFailed)
		},
		OnUnsubscribed: func(ctx context.Context) error {
			return c.Deps().SendMessage(ctx, cmdCtx.Room, c.Deps().Formatter.FormatMajorEventUnsubscribed(ctx))
		},
		OnStatus: func(ctx context.Context, subscribed bool) error {
			return c.Deps().SendMessage(ctx, cmdCtx.Room, c.Deps().Formatter.FormatMajorEventStatus(ctx, subscribed))
		},
	})
}

func (c *MajorEventCommand) handleSubscribe(ctx context.Context, cmdCtx *domain.CommandContext) error {
	if err := c.subscriptionFlow(cmdCtx).Subscribe(ctx, cmdCtx.Room, cmdCtx.RoomName); err != nil {
		return fmt.Errorf("subscribe: %w", err)
	}

	return nil
}

func (c *MajorEventCommand) handleUnsubscribe(ctx context.Context, cmdCtx *domain.CommandContext) error {
	if err := c.subscriptionFlow(cmdCtx).Unsubscribe(ctx, cmdCtx.Room); err != nil {
		return fmt.Errorf("unsubscribe: %w", err)
	}

	return nil
}

func (c *MajorEventCommand) handleStatus(ctx context.Context, cmdCtx *domain.CommandContext) error {
	if err := c.subscriptionFlow(cmdCtx).Status(ctx, cmdCtx.Room); err != nil {
		return fmt.Errorf("status: %w", err)
	}

	return nil
}
