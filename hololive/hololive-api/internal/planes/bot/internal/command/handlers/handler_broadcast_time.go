package handlers

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/botcommand"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers/handlercore"
)

// broadcastTimeLimit은 !방송시간이 보여 주는 최근 종료 방송 수다. 기간은 방송 이력 조회 상한(maxBroadcastHistoryDays)과 같다.
const broadcastTimeLimit = 5

// BroadcastTimeCommand는 한 멤버의 최근 종료 방송 시각을 방송 이력 저장소에서 조회한다.
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
	if err := c.ensureDeps(); err != nil {
		return fmt.Errorf("ensure dependencies: %w", err)
	}

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

	result, err := c.Deps().BroadcastHistory.ListEndedBroadcasts(ctx, &handlercore.BroadcastHistoryQuery{
		ChannelID: channel.ID,
		Limit:     broadcastTimeLimit,
		Since:     time.Now().AddDate(0, 0, -maxBroadcastHistoryDays),
	})
	if err != nil {
		c.Deps().Logger.Error("broadcast time query failed", slog.String("channel_id", channel.ID), slog.Any("error", err))

		if sendErr := c.Deps().SendMessage(ctx, cmdCtx.Room, "방송 시간 조회 중 오류가 발생했습니다."); sendErr != nil {
			return fmt.Errorf("send message: %w", sendErr)
		}

		return nil
	}

	message := c.Deps().Formatter.BroadcastTime(ctx, channel.Name, broadcastHistoryFormatterEntries(result.Entries), result.Truncated)
	if err := c.Deps().SendMessage(ctx, cmdCtx.Room, message); err != nil {
		return fmt.Errorf("send message: %w", err)
	}

	return nil
}

func (c *BroadcastTimeCommand) ensureDeps() error {
	if err := c.EnsureBaseDeps(); err != nil {
		return fmt.Errorf("ensure base dependencies: %w", err)
	}

	if c.Deps().BroadcastHistory == nil || c.Deps().Formatter == nil || c.Deps().Matcher == nil {
		return errors.New("broadcast time services not configured")
	}

	return nil
}
