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

	"github.com/kapu/hololive-shared/pkg/domain"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter"
)

type UpcomingCommand struct {
	BaseCommand
}

func NewUpcomingCommand(deps *Dependencies) *UpcomingCommand {
	return &UpcomingCommand{BaseCommand: NewBaseCommand(deps)}
}

func (c *UpcomingCommand) Name() string {
	return "upcoming"
}

func (c *UpcomingCommand) Description() string {
	return "예정된 방송 목록"
}

func (c *UpcomingCommand) Execute(ctx context.Context, cmdCtx *domain.CommandContext, params map[string]any) error {
	if err := c.ensureDeps(); err != nil {
		return fmt.Errorf("failed to ensure dependencies: %w", err)
	}

	options := parseUpcomingOptions(params)

	memberName, hasMember := params["member"].(string)
	if hasMember && memberName != "" {
		return c.executeMemberUpcoming(ctx, cmdCtx.Room, memberName, options.hours)
	}

	return c.executeAllUpcoming(ctx, cmdCtx.Room, options)
}

type upcomingOptions struct {
	hours        int
	displayLimit int
}

func parseUpcomingOptions(params map[string]any) upcomingOptions {
	hours := normalizeUpcomingHours(parseUpcomingIntParam(params, "hours", 24))
	showAll := boolParam(params, "all")
	displayLimit := normalizeUpcomingDisplayLimit(parseUpcomingIntParam(params, "limit", 10), showAll)

	return upcomingOptions{
		hours:        hours,
		displayLimit: displayLimit,
	}
}

func parseUpcomingIntParam(params map[string]any, key string, defaultValue int) int {
	raw, ok := params[key]
	if !ok {
		return defaultValue
	}

	switch v := raw.(type) {
	case int:
		return v
	case float64:
		return int(v)
	default:
		return defaultValue
	}
}

func normalizeUpcomingHours(hours int) int {
	if hours < 1 {
		return 24
	}

	if hours > 168 {
		return 168
	}

	return hours
}

func normalizeUpcomingDisplayLimit(displayLimit int, showAll bool) int {
	if showAll {
		return 0
	}

	if displayLimit < 1 {
		return 10
	}

	if displayLimit > 100 {
		return 100
	}

	return displayLimit
}

func (c *UpcomingCommand) executeMemberUpcoming(ctx context.Context, roomID, memberName string, hours int) error {
	channel, err := FindActiveMemberWithCandidatesOrError(ctx, c.Deps(), roomID, memberName)
	if memberLookupHandled(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to find member %q: %w", memberName, err)
	}
	if channel == nil {
		return nil
	}

	streams, err := c.Deps().Holodex.GetUpcomingStreams(ctx, hours)
	if err != nil {
		return c.Deps().SendError(ctx, roomID, adapter.ErrUpcomingStreamQueryFailed)
	}
	if streams == nil {
		streams = []*domain.Stream{}
	}

	memberStreams := filterUpcomingStreamsByChannel(streams, channel.ID)
	if len(memberStreams) == 0 {
		return c.Deps().SendMessage(ctx, roomID, c.Deps().Formatter.FormatMemberNoUpcoming(channel.Name, hours))
	}

	message := c.Deps().Formatter.UpcomingStreams(ctx, memberStreams, hours)

	return c.Deps().SendMessage(ctx, roomID, message)
}

func filterUpcomingStreamsByChannel(streams []*domain.Stream, channelID string) []*domain.Stream {
	memberStreams := make([]*domain.Stream, 0, len(streams))
	for _, stream := range streams {
		if stream == nil {
			continue
		}
		if stream.ChannelID == channelID {
			memberStreams = append(memberStreams, stream)
		}
	}
	return memberStreams
}

func (c *UpcomingCommand) executeAllUpcoming(ctx context.Context, roomID string, options upcomingOptions) error {
	streams, err := c.Deps().Holodex.GetUpcomingStreams(ctx, options.hours)
	if err != nil {
		return c.Deps().SendError(ctx, roomID, adapter.ErrUpcomingStreamQueryFailed)
	}
	if streams == nil {
		streams = []*domain.Stream{}
	}

	total := len(streams)
	if options.displayLimit > 0 && total > options.displayLimit {
		streams = streams[:options.displayLimit]
	}

	message := c.Deps().Formatter.UpcomingStreams(ctx, streams, options.hours)
	if options.displayLimit > 0 && total > options.displayLimit {
		message += c.Deps().Formatter.FormatUpcomingOverflowCount(total - options.displayLimit)
	}

	return c.Deps().SendMessage(ctx, roomID, message)
}

func (c *UpcomingCommand) ensureDeps() error {
	if err := c.EnsureBaseDeps(); err != nil {
		return fmt.Errorf("failed to ensure base dependencies: %w", err)
	}

	if c.Deps().Holodex == nil || c.Deps().Formatter == nil {
		return errors.New("upcoming command services not configured")
	}

	return nil
}
