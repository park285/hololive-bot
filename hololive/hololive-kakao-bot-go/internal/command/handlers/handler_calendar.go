package handlers

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/util"

	"github.com/kapu/hololive-kakao-bot-go/internal/adapter/messaging"
)

type CalendarCommand struct {
	BaseCommand
	memberRepo    CelebrationCalendarFinder
	imageRenderer CalendarImageRenderer
}

func NewCalendarCommand(deps *Dependencies, memberRepo CelebrationCalendarFinder, imageRenderer CalendarImageRenderer) *CalendarCommand {
	return &CalendarCommand{
		BaseCommand:   NewBaseCommand(deps),
		memberRepo:    memberRepo,
		imageRenderer: imageRenderer,
	}
}

func (c *CalendarCommand) Name() string {
	return "calendar"
}

func (c *CalendarCommand) Description() string {
	return "월별 기념일 달력 조회"
}

func (c *CalendarCommand) Execute(ctx context.Context, cmdCtx *domain.CommandContext, params map[string]any) error {
	if err := c.ensureDeps(); err != nil {
		return fmt.Errorf("calendar command: ensure deps: %w", err)
	}

	now := util.NowKST()
	month := int(now.Month())
	year := now.Year()

	if m, ok := params["month"].(int); ok && m >= 1 && m <= 12 {
		month = m
	}

	entries, err := c.memberRepo.FindMembersWithCelebrationsInMonth(ctx, month, year)
	if err != nil {
		c.Deps().Logger.Error("calendar query failed",
			slog.Int("month", month), slog.Int("year", year),
			slog.Any("error", err),
		)
		return c.Deps().SendError(ctx, cmdCtx.Room, messaging.ErrCalendarQueryFailed)
	}

	if c.imageRenderer != nil {
		if imgData, renderErr := c.imageRenderer.RenderCalendarImage(month, year, entries); renderErr == nil {
			return c.Deps().SendImage(ctx, cmdCtx.Room, imgData)
		} else {
			c.Deps().Logger.Warn("calendar image render failed, falling back to text",
				slog.Any("error", renderErr),
			)
		}
	}

	message := c.Deps().Formatter.CelebrationCalendar(ctx, month, year, entries)
	return c.Deps().SendMessage(ctx, cmdCtx.Room, message)
}

func (c *CalendarCommand) ensureDeps() error {
	if err := c.EnsureBaseDeps(); err != nil {
		return fmt.Errorf("calendar command: ensure base deps: %w", err)
	}
	if c.Deps().Formatter == nil {
		return errors.New("calendar command: formatter not configured")
	}
	if c.memberRepo == nil {
		return errors.New("calendar command: member repository not configured")
	}
	return nil
}
