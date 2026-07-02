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
	"math"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/util"
	"github.com/park285/shared-go/pkg/stringutil"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/render"
)

type StatsCommand struct {
	deps          *Dependencies
	imageRenderer RankImageRenderer
}

func NewStatsCommand(deps *Dependencies, imageRenderer RankImageRenderer) *StatsCommand {
	return &StatsCommand{deps: deps, imageRenderer: imageRenderer}
}

func (c *StatsCommand) Name() string {
	return "stats"
}

func (c *StatsCommand) Description() string {
	return "구독자 순위 및 통계 조회"
}

func (c *StatsCommand) Execute(ctx context.Context, cmdCtx *domain.CommandContext, params map[string]any) error {
	if err := c.ensureDeps(); err != nil {
		return fmt.Errorf("failed to ensure dependencies: %w", err)
	}

	action := stringParam(params, "action")
	if action == "" {
		action = "gainers"
	}

	switch stringutil.Normalize(action) {
	case "gainers", "구독자순위":
		return c.showTopGainers(ctx, cmdCtx, params)
	default:
		return c.deps.SendError(ctx, cmdCtx.Room, adapter.ErrUnknownStatsPeriod)
	}
}

func (c *StatsCommand) showTopGainers(ctx context.Context, cmdCtx *domain.CommandContext, params map[string]any) error {
	periodStr := stringParam(params, "period")
	now := time.Now()
	since, periodLabel := domain.ResolveStatsPeriod(now, periodStr)

	gainers, err := c.deps.StatsRepository.GetTopGainers(ctx, since, 10)
	if err != nil {
		c.deps.Logger.Error("Failed to get top gainers", slog.Any("error", err))
		return c.deps.SendError(ctx, cmdCtx.Room, adapter.ErrStatsQueryFailed)
	}

	if len(gainers) == 0 {
		return c.deps.SendError(ctx, cmdCtx.Room, adapter.MsgNoStatsData)
	}

	if c.trySendRankImage(ctx, cmdCtx.Room, periodLabel, gainers) {
		return nil
	}

	message := c.deps.Formatter.FormatStatsTopGainers(ctx, periodLabel, gainers)

	return c.deps.SendMessage(ctx, cmdCtx.Room, message)
}

func (c *StatsCommand) trySendRankImage(ctx context.Context, room, periodLabel string, gainers []domain.RankEntry) bool {
	if c.imageRenderer == nil {
		return false
	}

	imgData, err := c.imageRenderer.RenderRankImage(periodLabel, c.rankCardEntries(ctx, gainers))
	if err != nil {
		c.deps.Logger.Warn("rank image render failed, falling back to text",
			slog.Any("error", err),
		)
		return false
	}

	if err := c.deps.SendImage(ctx, room, imgData); err != nil {
		c.deps.Logger.Warn("rank image send failed, falling back to text",
			slog.Any("error", err),
		)
		return false
	}

	return true
}

func (c *StatsCommand) rankCardEntries(ctx context.Context, gainers []domain.RankEntry) []render.RankCardEntry {
	entries := make([]render.RankCardEntry, 0, len(gainers))
	for _, g := range gainers {
		entry := render.RankCardEntry{
			Rank:  g.Rank,
			Name:  g.MemberName,
			Delta: rankDeltaText(g.Value),
		}
		if g.CurrentSubscribers > 0 && g.CurrentSubscribers <= math.MaxInt64 {
			entry.Total = util.FormatKoreanNumber(int64(g.CurrentSubscribers))
		}
		if c.deps.Matcher != nil {
			if member := c.deps.Matcher.GetMemberByChannelID(ctx, g.ChannelID); member != nil {
				entry.Photo = member.Photo
				if member.ShortKoreanName != "" {
					entry.Name = member.ShortKoreanName
				}
			}
		}
		entries = append(entries, entry)
	}
	return entries
}

func rankDeltaText(value int64) string {
	formatted := util.FormatKoreanNumber(value)
	if value > 0 {
		return "+" + formatted
	}
	return formatted
}

func (c *StatsCommand) ensureDeps() error {
	if c == nil || c.deps == nil {
		return errors.New("stats command dependencies not configured")
	}

	if c.deps.SendMessage == nil || c.deps.SendError == nil {
		return errors.New("message callbacks not configured")
	}

	if c.deps.StatsRepository == nil {
		return errors.New("stats repository not configured")
	}

	if c.deps.Logger == nil {
		c.deps.Logger = slog.Default()
	}

	return nil
}
