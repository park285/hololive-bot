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
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/stringutil"

	"github.com/kapu/hololive-kakao-bot-go/internal/adapter"
)

type StatsCommand struct {
	deps *Dependencies
}

func NewStatsCommand(deps *Dependencies) *StatsCommand {
	return &StatsCommand{deps: deps}
}

func (c *StatsCommand) Name() string {
	return "stats"
}

func (c *StatsCommand) Description() string {
	return "구독자 순위 및 통계 조회"
}

func (c *StatsCommand) Execute(ctx context.Context, cmdCtx *domain.CommandContext, params map[string]any) error {
	if err := c.ensureDeps(cmdCtx); err != nil {
		return fmt.Errorf("failed to ensure dependencies: %w", err)
	}

	action, _ := params["action"].(string)
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
	periodStr, _ := params["period"].(string)
	now := time.Now()
	since, periodLabel := domain.ResolveStatsPeriod(now, periodStr)

	gainers, err := c.deps.StatsRepo.GetTopGainers(ctx, since, 10)
	if err != nil {
		c.deps.Logger.Error("Failed to get top gainers", slog.Any("error", err))
		return c.deps.SendError(ctx, cmdCtx.Room, adapter.ErrStatsQueryFailed)
	}

	if len(gainers) == 0 {
		return c.deps.SendMessage(ctx, cmdCtx.Room, adapter.MsgNoStatsData)
	}

	message := c.deps.Formatter.FormatStatsTopGainers(periodLabel, gainers)

	return c.deps.SendMessage(ctx, cmdCtx.Room, message)
}

func (c *StatsCommand) ensureDeps(cmdCtx *domain.CommandContext) error {
	if c == nil || c.deps == nil {
		return errors.New("stats command dependencies not configured")
	}

	if c.deps.SendMessage == nil || c.deps.SendError == nil {
		return errors.New("message callbacks not configured")
	}

	if c.deps.StatsRepo == nil {
		return errors.New("stats repository not configured")
	}

	if c.deps.Logger == nil {
		c.deps.Logger = slog.Default()
	}

	return nil
}
