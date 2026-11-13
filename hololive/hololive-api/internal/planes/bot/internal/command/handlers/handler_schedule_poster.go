package handlers

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/bot/orchestration/transport"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/botcommand"
	handlercore "github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers/handlercore"
)

// 포스터의 주 범위는 일본 방송 기준이라 오늘 날짜를 JST로 정한다.
var schedulePosterZone = time.FixedZone("JST", 9*60*60)

type SchedulePosterCommand struct {
	handlercore.BaseCommand

	now func() time.Time
}

func NewSchedulePosterCommand(deps *handlercore.Dependencies) *SchedulePosterCommand {
	return &SchedulePosterCommand{BaseCommand: handlercore.NewBaseCommand(deps), now: time.Now}
}

func (c *SchedulePosterCommand) Name() string {
	return "schedule_poster"
}

func (c *SchedulePosterCommand) Description() string {
	return "멤버 주간 일정 포스터 조회"
}

func (c *SchedulePosterCommand) Execute(ctx context.Context, cmdCtx *botcommand.CommandContext, params map[string]any) error {
	room := cmdCtx.Room

	memberName, ok := scheduleMemberName(params)
	if !ok {
		return c.replyError(ctx, room, messaging.ErrSchedulePosterNeedMemberName)
	}

	channel, err := handlercore.FindActiveMemberWithCandidatesOrError(ctx, c.Deps(), room, memberName, "스케쥴")
	if memberLookupHandled(err) {
		return nil
	}

	if err != nil {
		return fmt.Errorf("find member: %w", err)
	}

	if channel == nil {
		return nil
	}

	now := c.now().In(schedulePosterZone)
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)

	image, found, err := c.Deps().SchedulePosters.PublishedImage(ctx, channel.ID, day)
	if err != nil {
		c.Deps().Logger.Error("Failed to load schedule poster", slog.String("channel_id", channel.ID), slog.Any("error", err))

		return c.replyError(ctx, room, messaging.ErrSchedulePosterQueryFailed)
	}

	if !found {
		text := c.Deps().Formatter.SchedulePosterNotFound()
		if text == "" {
			return errors.New("send schedule poster not found: message_strings misc/schedule_poster_not_found has no text")
		}

		return c.reply(ctx, room, text)
	}

	if err := c.Deps().SendImage(transport.WithImageContentType(ctx, image.MIME), room, image.Data); err != nil {
		return fmt.Errorf("send schedule poster: %w", err)
	}

	return nil
}

func (c *SchedulePosterCommand) CheckDependencies() error {
	if err := c.EnsureBaseDeps(); err != nil {
		return fmt.Errorf("ensure base dependencies: %w", err)
	}

	if c.Deps().Matcher == nil || c.Deps().SchedulePosters == nil || c.Deps().Formatter == nil || c.Deps().SendImage == nil {
		return errors.New("schedule poster command services not configured")
	}

	return nil
}

func (c *SchedulePosterCommand) reply(ctx context.Context, room, message string) error {
	if err := c.Deps().SendMessage(ctx, room, message); err != nil {
		return fmt.Errorf("send message: %w", err)
	}

	return nil
}

func (c *SchedulePosterCommand) replyError(ctx context.Context, room, key string) error {
	if err := c.Deps().SendError(ctx, room, key); err != nil {
		return fmt.Errorf("send error: %w", err)
	}

	return nil
}
