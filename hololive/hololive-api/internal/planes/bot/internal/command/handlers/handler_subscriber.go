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

	"github.com/park285/shared-go/v2/pkg/stringutil"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging"
	handlercore "github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers/handlercore"
	"github.com/kapu/hololive-shared/pkg/domain"
)

type SubscriberCommand struct {
	handlercore.BaseCommand
}

func NewSubscriberCommand(deps *handlercore.Dependencies) *SubscriberCommand {
	return &SubscriberCommand{BaseCommand: handlercore.NewBaseCommand(deps)}
}

func (c *SubscriberCommand) Name() string {
	return string(domain.CommandSubscriber)
}

func (c *SubscriberCommand) Description() string {
	return "특정 멤버의 구독자 수 조회"
}

func (c *SubscriberCommand) Execute(ctx context.Context, cmdCtx *domain.CommandContext, params map[string]any) error {
	if err := c.ensureDeps(); err != nil {
		return fmt.Errorf("failed to ensure dependencies: %w", err)
	}

	memberQuery := stringParam(params, paramMember)

	memberQuery = stringutil.TrimSpace(memberQuery)

	// 멤버 이름 필수
	if memberQuery == "" {
		if err := c.Deps().SendError(ctx, cmdCtx.Room, messaging.ErrSubscriberNeedMemberName); err != nil {
			return fmt.Errorf("send error: %w", err)
		}

		return nil
	}

	if err := c.executeSubscriberLookup(ctx, cmdCtx.Room, memberQuery); err != nil {
		return fmt.Errorf("execute subscriber lookup: %w", err)
	}

	return nil
}

func (c *SubscriberCommand) executeSubscriberLookup(ctx context.Context, room, memberQuery string) error {
	matchedChannel, err := handlercore.FindMemberWithCandidatesOrError(ctx, c.Deps(), room, memberQuery, "구독자")
	if memberLookupHandled(err) {
		return nil
	}

	if err != nil {
		return fmt.Errorf("failed to find member: %w", err)
	}

	if matchedChannel == nil {
		return nil
	}

	channel, err := c.getSubscriberChannel(ctx, matchedChannel.ID)
	if err != nil {
		if err := c.Deps().SendError(ctx, room, messaging.ErrSubscriberQueryFailed); err != nil {
			return fmt.Errorf("send error: %w", err)
		}

		return nil
	}

	if err := c.sendSubscriberCount(ctx, room, channel); err != nil {
		return fmt.Errorf("send subscriber count: %w", err)
	}

	return nil
}

func (c *SubscriberCommand) sendSubscriberCount(ctx context.Context, room string, channel *domain.Channel) error {
	if !hasSubscriberCount(channel) || *channel.SubscriberCount < 0 {
		if err := c.Deps().SendError(ctx, room, messaging.MsgNoSubscriberData); err != nil {
			return fmt.Errorf("send error: %w", err)
		}

		return nil
	}

	memberName := c.subscriberMemberName(ctx, channel)
	message := c.Deps().Formatter.FormatSubscriberCount(ctx, memberName, uint64(*channel.SubscriberCount))

	if err := c.Deps().SendMessage(ctx, room, message); err != nil {
		return fmt.Errorf("send message: %w", err)
	}

	return nil
}

func (c *SubscriberCommand) getSubscriberChannel(ctx context.Context, channelID string) (*domain.Channel, error) {
	channel, err := c.Deps().Holodex.GetChannel(ctx, channelID)
	if err != nil {
		c.log().Error("Failed to get channel from Holodex",
			slog.String("channel_id", channelID),
			slog.Any("error", err),
		)

		return nil, fmt.Errorf("get channel from holodex: %w", err)
	}

	return channel, nil
}

func hasSubscriberCount(channel *domain.Channel) bool {
	return channel != nil && channel.SubscriberCount != nil && *channel.SubscriberCount != 0
}

func (c *SubscriberCommand) subscriberMemberName(ctx context.Context, channel *domain.Channel) string {
	provider := c.Deps().MembersData.WithContext(ctx)
	member := provider.FindMemberByChannelID(channel.ID)

	if member != nil && member.NameKo != "" {
		return member.NameKo
	}

	if channel.EnglishName != nil && *channel.EnglishName != "" {
		return *channel.EnglishName
	}

	return channel.Name
}

func (c *SubscriberCommand) ensureDeps() error {
	if err := c.EnsureBaseDeps(); err != nil {
		return fmt.Errorf("failed to ensure base dependencies: %w", err)
	}

	if c.Deps().Matcher == nil {
		return errors.New("matcher not configured")
	}

	if c.Deps().Holodex == nil {
		return errors.New("holodex service not configured")
	}

	if c.Deps().MembersData == nil {
		return errors.New("members data not configured")
	}

	if c.Deps().Formatter == nil {
		return errors.New("formatter not configured")
	}

	return nil
}

func (c *SubscriberCommand) log() *slog.Logger {
	if c.Deps() != nil && c.Deps().Logger != nil {
		return c.Deps().Logger
	}

	return slog.Default()
}
