package handlers

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/bot/orchestration/transport"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/botcommand"
	handlercore "github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers/handlercore"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/timeutil"
)

const calendarCardContentType = "image/png"

type CalendarCommand struct {
	handlercore.BaseCommand

	memberRepo    handlercore.CelebrationCalendarFinder
	imageRenderer handlercore.CalendarImageRenderer
	now           func() time.Time
}

func NewCalendarCommand(deps *handlercore.Dependencies, memberRepo handlercore.CelebrationCalendarFinder, imageRenderer handlercore.CalendarImageRenderer) *CalendarCommand {
	return &CalendarCommand{
		BaseCommand:   handlercore.NewBaseCommand(deps),
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

func (c *CalendarCommand) Execute(ctx context.Context, cmdCtx *botcommand.CommandContext, params map[string]any) error {
	month, year := c.targetMonthYear(params)

	entries, err := c.memberRepo.FindMembersWithCelebrationsInMonth(ctx, month, year)
	if err != nil {
		c.Deps().Logger.Error("calendar query failed",
			slog.Int("month", month), slog.Int("year", year),
			slog.Any("error", err),
		)

		if err := c.Deps().SendError(ctx, cmdCtx.Room, messaging.ErrCalendarQueryFailed); err != nil {
			return fmt.Errorf("send error: %w", err)
		}

		return nil
	}

	imageSent, imageErr := c.trySendCalendarImages(ctx, cmdCtx.Room, month, year, entries)
	if imageErr != nil {
		return fmt.Errorf("send calendar image: %w", imageErr)
	}

	if imageSent {
		return nil
	}

	message := c.Deps().Formatter.CelebrationCalendar(ctx, month, year, entries)

	if err := c.Deps().SendMessage(ctx, cmdCtx.Room, message); err != nil {
		return fmt.Errorf("send message: %w", err)
	}

	return nil
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
		return timeutil.ToKST(c.now())
	}

	return timeutil.NowKST()
}

func (c *CalendarCommand) trySendCalendarImages(ctx context.Context, room string, month, year int, entries []domain.CalendarEntry) (bool, error) {
	pages, err := c.renderCalendarImages(ctx, month, year, entries)
	if err != nil || len(pages) == 0 || slices.ContainsFunc(pages, func(p []byte) bool { return len(p) == 0 }) {
		observeImageTextFallback(c.Name(), imageTextFallbackReasonRenderFailed)
		c.Deps().Logger.Warn("calendar image render failed, falling back to text",
			slog.Any("error", err),
		)

		return false, nil
	}

	sendCtx := ctx

	if len(pages) == 1 {
		sendCtx = transport.WithImageContentType(ctx, calendarCardContentType)
	}

	if err := c.Deps().SendImages(sendCtx, room, pages); err != nil {
		if transport.IsReplyOutcomeUnknown(err) {
			observeImageTextFallback(c.Name(), imageTextFallbackReasonOutcomeUnknown)
			c.Deps().Logger.Warn("calendar image outcome unknown, suppressing text fallback",
				slog.Any("error", err),
			)

			return false, fmt.Errorf("calendar image outcome unknown: %w", err)
		}

		observeImageTextFallback(c.Name(), imageTextFallbackReasonSendFailed)
		c.Deps().Logger.Warn("calendar image send failed, falling back to text",
			slog.Any("error", err),
		)

		return false, nil
	}

	return true, nil
}

func (c *CalendarCommand) renderCalendarImages(ctx context.Context, month, year int, entries []domain.CalendarEntry) ([][]byte, error) {
	out, err := c.imageRenderer.RenderCalendarImagesContext(ctx, month, year, entries)
	if err != nil {
		return out, fmt.Errorf("render calendar images context: %w", err)
	}

	return out, nil
}

func (c *CalendarCommand) CheckDependencies() error {
	if err := c.EnsureBaseDeps(); err != nil {
		return fmt.Errorf("calendar command: ensure base deps: %w", err)
	}

	if c.Deps().Formatter == nil {
		return errors.New("calendar command: formatter not configured")
	}

	if c.memberRepo == nil {
		return errors.New("calendar command: member repository not configured")
	}

	if c.imageRenderer == nil || c.Deps().SendImages == nil {
		return errors.New("calendar command: image renderer not configured")
	}

	return nil
}
