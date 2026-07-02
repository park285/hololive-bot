package handlers

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/util"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter"
)

type CalendarCommand struct {
	BaseCommand
	memberRepo    CelebrationCalendarFinder
	imageRenderer CalendarImageRenderer
	now           func() time.Time
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
	return "기념일 달력 조회: 이번달/다음달/저번달"
}

func (c *CalendarCommand) Execute(ctx context.Context, cmdCtx *domain.CommandContext, params map[string]any) error {
	if err := c.ensureDeps(); err != nil {
		return fmt.Errorf("calendar command: ensure deps: %w", err)
	}

	month, year := c.targetMonthYear(params)

	entries, err := c.memberRepo.FindMembersWithCelebrationsInMonth(ctx, month, year)
	if err != nil {
		c.Deps().Logger.Error("calendar query failed",
			slog.Int("month", month), slog.Int("year", year),
			slog.Any("error", err),
		)
		return c.Deps().SendError(ctx, cmdCtx.Room, adapter.ErrCalendarQueryFailed)
	}

	if c.trySendCalendarImage(ctx, cmdCtx.Room, month, year, entries) {
		return nil
	}

	message := c.Deps().Formatter.CelebrationCalendar(ctx, month, year, entries)
	return c.Deps().SendMessage(ctx, cmdCtx.Room, message)
}

func (c *CalendarCommand) targetMonthYear(params map[string]any) (month, year int) {
	now := c.nowKST()
	month = int(now.Month())
	year = now.Year()

	if m, ok := params["month"].(int); ok && m >= 1 && m <= 12 {
		return m, year
	}
	if offset, ok := params["monthOffset"].(int); ok && offset != 0 {
		base := time.Date(year, now.Month(), 1, 0, 0, 0, 0, now.Location())
		target := base.AddDate(0, offset, 0)
		return int(target.Month()), target.Year()
	}

	return month, year
}

func (c *CalendarCommand) nowKST() time.Time {
	if c.now != nil {
		return util.ToKST(c.now())
	}
	return util.NowKST()
}

func (c *CalendarCommand) trySendCalendarImage(ctx context.Context, room string, month, year int, entries []domain.CalendarEntry) bool {
	if c.imageRenderer == nil {
		return false
	}

	pages, err := c.imageRenderer.RenderCalendarImages(month, year, entries)
	if err != nil || len(pages) == 0 {
		c.Deps().Logger.Warn("calendar image render failed, falling back to text",
			slog.Any("error", err),
		)
		return false
	}

	if err := c.sendCalendarPages(ctx, room, pages); err != nil {
		c.Deps().Logger.Warn("calendar image send failed, falling back to text",
			slog.Any("error", err),
		)
		return false
	}

	return true
}

func (c *CalendarCommand) sendCalendarPages(ctx context.Context, room string, pages [][]byte) error {
	if len(pages) == 1 {
		return c.Deps().SendImage(ctx, room, pages[0])
	}
	return c.Deps().SendMultipleImages(ctx, room, pages)
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
