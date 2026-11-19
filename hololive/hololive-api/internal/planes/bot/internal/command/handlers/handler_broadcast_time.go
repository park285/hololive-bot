package handlers

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging/formatter"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/botcommand"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers/handlercore"
)

const (
	defaultBroadcastTimeDays = 7
	broadcastTimeMaxEntries  = maxBroadcastHistoryQueryLimit
)

type BroadcastTimeCommand struct {
	handlercore.BaseCommand
}

func NewBroadcastTimeCommand(deps *handlercore.Dependencies) *BroadcastTimeCommand {
	return &BroadcastTimeCommand{BaseCommand: handlercore.NewBaseCommand(deps)}
}

func (c *BroadcastTimeCommand) Name() string {
	return "broadcast_time"
}

func (c *BroadcastTimeCommand) Description() string {
	return "특정 멤버의 최근 방송 시각 조회"
}

func (c *BroadcastTimeCommand) Execute(ctx context.Context, cmdCtx *botcommand.CommandContext, params map[string]any) error {
	memberName := stringParam(params, paramMember)
	if memberName == "" {
		if err := c.Deps().SendMessage(ctx, cmdCtx.Room, c.Deps().Formatter.BroadcastTimeUsage()); err != nil {
			return fmt.Errorf("send message: %w", err)
		}

		return nil
	}

	channel, err := handlercore.FindActiveMemberWithCandidatesOrError(ctx, c.Deps(), cmdCtx.Room, memberName, "방송시간")
	if memberLookupHandled(err) {
		return nil
	}

	if err != nil {
		return fmt.Errorf("find member: %w", err)
	}

	days := normalizeBroadcastHistoryDays(intParam(params, paramDays, defaultBroadcastTimeDays))
	now := time.Now()

	result, err := c.Deps().BroadcastHistory.ListEndedBroadcasts(ctx, &handlercore.BroadcastHistoryQuery{
		ChannelID: channel.ID,
		Limit:     broadcastTimeMaxEntries,
		Since:     now.AddDate(0, 0, -days),
	})
	if err != nil {
		c.Deps().Logger.Error("broadcast time query failed", slog.String("channel_id", channel.ID), slog.Any("error", err))

		if sendErr := c.Deps().SendError(ctx, cmdCtx.Room, messaging.ErrBroadcastTimeQueryFailed); sendErr != nil {
			return fmt.Errorf("send message: %w", sendErr)
		}

		return nil
	}

	message := c.Deps().Formatter.BroadcastTime(ctx, &formatter.BroadcastTimeSummary{
		MemberName: channel.Name,
		Days:       days,
		Entries:    broadcastHistoryFormatterEntries(result.Entries),
		Partial:    result.Truncated || len(result.Entries) >= broadcastTimeMaxEntries,
		Now:        now,
	})
	if err := c.Deps().SendMessage(ctx, cmdCtx.Room, message); err != nil {
		return fmt.Errorf("send message: %w", err)
	}

	return nil
}

func (c *BroadcastTimeCommand) CheckDependencies() error {
	if err := c.EnsureBaseDeps(); err != nil {
		return fmt.Errorf("ensure base dependencies: %w", err)
	}

	if c.Deps().BroadcastHistory == nil || c.Deps().Formatter == nil || c.Deps().Matcher == nil {
		return errors.New("broadcast time services not configured")
	}

	return nil
}
