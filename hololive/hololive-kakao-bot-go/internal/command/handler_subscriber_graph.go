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

package command

import (
	"errors"
	"context"
	"fmt"
	"log/slog"
	"strconv"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/stats"

	"github.com/kapu/hololive-kakao-bot-go/internal/adapter"
)

type SubscriberGraphCommand struct {
	BaseCommand
}

func NewSubscriberGraphCommand(deps *Dependencies) *SubscriberGraphCommand {
	return &SubscriberGraphCommand{BaseCommand: NewBaseCommand(deps)}
}

func (c *SubscriberGraphCommand) Name() string {
	return "subscribergraph"
}

func (c *SubscriberGraphCommand) Description() string {
	return "멤버의 구독자 추이 그래프"
}

func (c *SubscriberGraphCommand) Execute(ctx context.Context, cmdCtx *domain.CommandContext, params map[string]any) error {
	if err := c.ensureDeps(); err != nil {
		return fmt.Errorf("failed to ensure dependencies: %w", err)
	}

	memberName, _ := params["member"].(string)
	if memberName == "" {
		return c.Deps().SendError(ctx, cmdCtx.Room, adapter.ErrGraphNeedMemberName)
	}

	channel, err := FindActiveMemberOrError(ctx, c.Deps(), cmdCtx.Room, memberName)
	if err != nil {
		return fmt.Errorf("failed to find member %q: %w", memberName, err)
	}

	days := 30

	if daysStr, ok := params["days"].(string); ok && daysStr != "" {
		if parsed, parseErr := strconv.Atoi(daysStr); parseErr == nil && parsed > 0 {
			days = parsed
		}
	}

	if days > 90 {
		days = 90
	}

	graphData, err := c.Deps().StatsRepo.GetSubscriberGraph(ctx, channel.ID, days)
	if err != nil {
		c.Deps().Logger.Error("Failed to get subscriber graph", slog.Any("error", err))
		return c.Deps().SendError(ctx, cmdCtx.Room, adapter.ErrGraphQueryFailed)
	}

	if graphData == nil || len(graphData.Points) == 0 {
		return c.Deps().SendMessage(ctx, cmdCtx.Room, adapter.MsgNoGraphData)
	}

	message := c.Deps().Formatter.FormatSubscriberGraph(
		channel.Name,
		days,
		graphData.Current,
		graphData.Change7d,
		graphData.Change30d,
		graphData.SampleCount,
		graphData.UpdatedAt,
		graphPointValues(graphData.Points),
	)

	return c.Deps().SendMessage(ctx, cmdCtx.Room, message)
}

func graphPointValues(points []stats.SubscriberGraphPoint) []int64 {
	if len(points) == 0 {
		return nil
	}

	values := make([]int64, len(points))
	for i, point := range points {
		values[i] = point.Subscribers
	}

	return values
}

func (c *SubscriberGraphCommand) ensureDeps() error {
	if err := c.EnsureBaseDeps(); err != nil {
		return fmt.Errorf("failed to ensure base dependencies: %w", err)
	}

	if c.Deps().Matcher == nil || c.Deps().StatsRepo == nil {
		return errors.New("subscriber graph command services not configured")
	}

	return nil
}
