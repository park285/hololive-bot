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
	"github.com/kapu/hololive-api/internal/planes/bot/internal/render/schedulecard"
	"github.com/kapu/hololive-api/internal/service/scheduleposter"
	"github.com/kapu/hololive-api/internal/service/scheduleposter/cardtemplate"
	"github.com/kapu/hololive-shared/pkg/domain"
)

var schedulePosterZone = time.FixedZone("JST", 9*60*60)

const scheduleCardContentType = "image/png"

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

func (c *SchedulePosterCommand) Execute(ctx context.Context, cmdCtx *botcommand.CommandContext, params botcommand.SchedulePosterParams) error {
	room := cmdCtx.Room

	if params.Member == "" {
		return c.replyError(ctx, room, messaging.ErrSchedulePosterNeedMemberName)
	}

	member, err := handlercore.FindActiveMemberProfile(ctx, c.Deps(), room, params.Member, "스케쥴")
	if memberLookupHandled(err) {
		return nil
	}

	if err != nil {
		return fmt.Errorf("find member: %w", err)
	}

	if member == nil {
		return nil
	}

	now := c.now().In(schedulePosterZone)
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)

	schedule, template, found, err := c.Deps().SchedulePosters.PublishedMemberRows(ctx, member.ID, member.ChannelID, day)
	if err != nil {
		c.Deps().Logger.Error("Failed to load schedule poster", slog.String("channel_id", member.ChannelID), slog.Any("error", err))

		return c.replyError(ctx, room, messaging.ErrSchedulePosterQueryFailed)
	}

	if !found {
		text := c.Deps().Formatter.SchedulePosterNotFound()
		if text == "" {
			return errors.New("send schedule poster not found: message_strings misc/schedule_poster_not_found has no text")
		}

		return c.reply(ctx, room, text)
	}

	return c.replySchedule(ctx, room, member, schedule, template)
}

func (c *SchedulePosterCommand) replySchedule(ctx context.Context, room string, member *domain.Member, schedule scheduleposter.PublishedSchedule, template cardtemplate.ID) error {
	pages, err := c.Deps().ScheduleCards.RenderScheduleCards(ctx, member, schedule, template)
	if errors.Is(err, schedulecard.ErrRenderBudget) {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}

		c.Deps().Logger.InfoContext(ctx, "sending complete schedule as text file", slog.Int64("poster_id", schedule.PosterID), slog.Int("member_id", member.ID))

		if fileErr := c.Deps().SendTextFile(ctx, room, "schedule.txt", scheduleText(member.DisplayName(), &schedule)); fileErr != nil {
			return fmt.Errorf("send complete schedule file: %w", fileErr)
		}

		return nil
	}

	if err != nil {
		return fmt.Errorf("render schedule card: %w", err)
	}

	if len(pages) == 1 {
		ctx = transport.WithImageContentType(ctx, scheduleCardContentType)
	}

	if err := c.Deps().SendImages(ctx, room, pages); err != nil {
		return fmt.Errorf("send schedule card: %w", err)
	}

	return nil
}

func (c *SchedulePosterCommand) CheckDependencies() error {
	if err := c.EnsureBaseDeps(); err != nil {
		return fmt.Errorf("ensure base dependencies: %w", err)
	}

	if c.Deps().Matcher == nil || c.Deps().SchedulePosters == nil || c.Deps().ScheduleCards == nil || c.Deps().Formatter == nil || c.Deps().SendImages == nil || c.Deps().SendTextFile == nil {
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
