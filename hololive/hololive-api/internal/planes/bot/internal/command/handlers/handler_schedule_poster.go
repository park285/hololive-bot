package handlers

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/bot/orchestration/transport"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/botcommand"
	handlercore "github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers/handlercore"
)

const (
	schedulePosterNeedMember  = "❌ 멤버 이름을 입력해주세요.\n예) !스케쥴 소피아"
	schedulePosterQueryFailed = "❌ 일정 포스터 조회 중 오류가 발생했습니다."
	schedulePosterNotFound    = "이번 주 번역된 일정 포스터가 아직 없습니다."
)

// 포스터의 주 범위는 일본 방송 기준이라 오늘 날짜를 JST로 정한다.
var (
	schedulePosterZone = time.FixedZone("JST", 9*60*60)
	schedulePosterNow  = time.Now
)

type SchedulePosterCommand struct {
	handlercore.BaseCommand
}

func NewSchedulePosterCommand(deps *handlercore.Dependencies) *SchedulePosterCommand {
	return &SchedulePosterCommand{BaseCommand: handlercore.NewBaseCommand(deps)}
}

func (c *SchedulePosterCommand) Name() string {
	return "schedule_poster"
}

func (c *SchedulePosterCommand) Description() string {
	return "멤버 주간 일정 포스터 조회"
}

func (c *SchedulePosterCommand) Execute(ctx context.Context, cmdCtx *botcommand.CommandContext, params map[string]any) error {
	if err := c.EnsureBaseDeps(); err != nil {
		return fmt.Errorf("ensure base dependencies: %w", err)
	}

	if c.Deps().Matcher == nil || c.Deps().SchedulePosters == nil {
		return errors.New("schedule poster command services not configured")
	}

	room := cmdCtx.Room

	memberName, ok := scheduleMemberName(params)
	if !ok {
		return c.reply(ctx, room, schedulePosterNeedMember)
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

	now := schedulePosterNow().In(schedulePosterZone)
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)

	image, found, err := c.Deps().SchedulePosters.PublishedImage(ctx, channel.ID, day)
	if err != nil {
		c.Deps().Logger.Error("Failed to load schedule poster", slog.String("channel_id", channel.ID), slog.Any("error", err))

		return c.reply(ctx, room, schedulePosterQueryFailed)
	}

	if !found {
		return c.reply(ctx, room, schedulePosterNotFound)
	}

	if err := c.Deps().SendImage(transport.WithImageContentType(ctx, image.MIME), room, image.Data); err != nil {
		return fmt.Errorf("send schedule poster: %w", err)
	}

	return nil
}

func (c *SchedulePosterCommand) reply(ctx context.Context, room, message string) error {
	if err := c.Deps().SendMessage(ctx, room, message); err != nil {
		return fmt.Errorf("send message: %w", err)
	}

	return nil
}
