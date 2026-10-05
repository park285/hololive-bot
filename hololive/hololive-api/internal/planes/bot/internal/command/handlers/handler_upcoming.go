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

	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging"
	handlercore "github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers/handlercore"
	"github.com/kapu/hololive-shared/pkg/domain"
)

type UpcomingCommand struct {
	handlercore.BaseCommand
}

func NewUpcomingCommand(deps *handlercore.Dependencies) *UpcomingCommand {
	return &UpcomingCommand{BaseCommand: handlercore.NewBaseCommand(deps)}
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

	memberName, hasMember := params[paramMember].(string)
	if hasMember && memberName != "" {
		if err := c.executeMemberUpcoming(ctx, cmdCtx.Room, memberName, options.hours); err != nil {
			return fmt.Errorf("execute member upcoming: %w", err)
		}

		return nil
	}

	if err := c.executeAllUpcoming(ctx, cmdCtx.Room, options); err != nil {
		return fmt.Errorf("execute all upcoming: %w", err)
	}

	return nil
}

type upcomingOptions struct {
	hours        int
	displayLimit int
}

func parseUpcomingOptions(params map[string]any) upcomingOptions {
	hours := normalizeUpcomingHours(parseUpcomingIntParam(params, "hours", 24))
	showAll := boolParam(params, "all")
	displayLimit := normalizeUpcomingDisplayLimit(parseUpcomingIntParam(params, "limit", 0), showAll)

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
		return 0
	}

	if displayLimit > 100 {
		return 100
	}

	return displayLimit
}

func (c *UpcomingCommand) executeMemberUpcoming(ctx context.Context, roomID, memberName string, hours int) error {
	channel, err := handlercore.FindActiveMemberWithCandidatesOrError(ctx, c.Deps(), roomID, memberName, "예정")
	if memberLookupHandled(err) {
		return nil
	}

	if err != nil {
		return fmt.Errorf("failed to find member: %w", err)
	}

	if channel == nil {
		return nil
	}

	if err := c.sendMemberUpcomingStreams(ctx, roomID, channel, hours); err != nil {
		return fmt.Errorf("send member upcoming streams: %w", err)
	}

	return nil
}

func (c *UpcomingCommand) sendMemberUpcomingStreams(ctx context.Context, roomID string, channel *domain.Channel, hours int) error {
	streams, err := c.Deps().Holodex.GetUpcomingStreams(ctx, hours)
	if err != nil {
		if sendErr := c.Deps().SendError(ctx, roomID, messaging.ErrUpcomingStreamQueryFailed); sendErr != nil {
			return fmt.Errorf("send error: %w", sendErr)
		}

		return nil
	}

	if streams == nil {
		streams = []*domain.Stream{}
	}

	memberStreams, err := c.withMemberDisplayNames(ctx, filterUpcomingStreamsByChannel(streams, channel.ID))
	if err != nil {
		return c.replyDisplayNameFailure(ctx, roomID, err)
	}

	if len(memberStreams) == 0 {
		if err := c.Deps().SendMessage(ctx, roomID, c.Deps().Formatter.FormatMemberNoUpcoming(ctx, channel.Name, hours)); err != nil {
			return fmt.Errorf("send message: %w", err)
		}

		return nil
	}

	message := c.Deps().Formatter.UpcomingStreams(ctx, memberStreams, hours)

	if err := c.Deps().SendMessage(ctx, roomID, message); err != nil {
		return fmt.Errorf("send message: %w", err)
	}

	return nil
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
		if sendErr := c.Deps().SendError(ctx, roomID, messaging.ErrUpcomingStreamQueryFailed); sendErr != nil {
			return fmt.Errorf("send error: %w", sendErr)
		}

		return nil
	}

	if streams == nil {
		streams = []*domain.Stream{}
	}

	if options.displayLimit > 0 && len(streams) > options.displayLimit {
		streams = streams[:options.displayLimit]
	}

	streams, err = c.withMemberDisplayNames(ctx, streams)
	if err != nil {
		return c.replyDisplayNameFailure(ctx, roomID, err)
	}

	message := c.Deps().Formatter.UpcomingStreams(ctx, streams, options.hours)

	if err := c.Deps().SendMessage(ctx, roomID, message); err != nil {
		return fmt.Errorf("send message: %w", err)
	}

	return nil
}

// withMemberDisplayNames는 members에 등록된 채널의 스트림 이름을 명령 응답 표시명으로 바꾼 사본을 돌려준다.
// Holodex 응답은 캐시와 공유될 수 있어 원본을 고치지 않는다. 등록되지 않은 채널은 기존처럼 응답 이름을 그대로 둔다.
func (c *UpcomingCommand) withMemberDisplayNames(ctx context.Context, streams []*domain.Stream) ([]*domain.Stream, error) {
	channelIDs := make([]string, 0, len(streams))
	for _, stream := range streams {
		if stream != nil {
			channelIDs = append(channelIDs, stream.ChannelID)
		}
	}

	names, err := c.Deps().Matcher.MemberDisplayNames(ctx, channelIDs)
	if err != nil {
		return nil, fmt.Errorf("resolve member display names: %w", err)
	}

	named := make([]*domain.Stream, len(streams))
	for i, stream := range streams {
		named[i] = stream
		if stream == nil {
			continue
		}

		if name, ok := names[stream.ChannelID]; ok {
			renamed := *stream

			renamed.ChannelName = name
			named[i] = &renamed
		}
	}

	return named, nil
}

// replyDisplayNameFailure는 표시명 조회 실패를 원천 이름으로 덮지 않고 예정 조회 실패로 알린다.
func (c *UpcomingCommand) replyDisplayNameFailure(ctx context.Context, roomID string, cause error) error {
	c.Deps().Logger.Error("Failed to resolve upcoming member display names", slog.Any("error", cause))

	if err := c.Deps().SendError(ctx, roomID, messaging.ErrUpcomingStreamQueryFailed); err != nil {
		return fmt.Errorf("send error: %w", err)
	}

	return nil
}

func (c *UpcomingCommand) ensureDeps() error {
	if err := c.EnsureBaseDeps(); err != nil {
		return fmt.Errorf("failed to ensure base dependencies: %w", err)
	}

	if c.Deps().Holodex == nil || c.Deps().Formatter == nil || c.Deps().Matcher == nil {
		return errors.New("upcoming command services not configured")
	}

	return nil
}
