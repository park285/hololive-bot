package handlers

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/bot/orchestration/transport"
	handlercore "github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers/handlercore"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/timeutil"
)

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

		if err := c.Deps().SendError(ctx, cmdCtx.Room, messaging.ErrCalendarQueryFailed); err != nil {
			return fmt.Errorf("send error: %w", err)
		}

		return nil
	}

	imageSent, imageErr := c.trySendCalendarImage(ctx, cmdCtx.Room, month, year, entries)
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

// trySendCalendarImage는 이미지를 보냈으면 true, 렌더링·전송이 확정적으로 실패하면 false를 돌려 텍스트 달력을 보내게 한다.
// 전송 결과가 불명이면 이미지가 이미 전달됐을 수 있으므로 텍스트 없이 오류로 올린다.
func (c *CalendarCommand) trySendCalendarImage(ctx context.Context, room string, month, year int, entries []domain.CalendarEntry) (bool, error) {
	data, err := c.renderCalendarImage(ctx, month, year, entries)
	if err != nil || len(data) == 0 {
		observeImageTextFallback(c.Name(), imageTextFallbackReasonRenderFailed)
		c.Deps().Logger.Warn("calendar image render failed, falling back to text",
			slog.Any("error", err),
		)

		return false, nil
	}

	if err := c.Deps().SendImage(ctx, room, data); err != nil {
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

func (c *CalendarCommand) renderCalendarImage(ctx context.Context, month, year int, entries []domain.CalendarEntry) ([]byte, error) {
	out, err := c.imageRenderer.RenderCalendarImageContext(ctx, month, year, entries)
	if err != nil {
		return out, fmt.Errorf("render calendar image context: %w", err)
	}

	return out, nil
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

	// 운영 조립은 달력 이미지 renderer와 이미지 전송을 항상 연결한다. 없으면 텍스트로 조용히 바꾸지 않고 설정 오류로 드러낸다.
	if c.imageRenderer == nil || c.Deps().SendImage == nil {
		return errors.New("calendar command: image renderer not configured")
	}

	return nil
}
