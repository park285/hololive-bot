package handlers

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/botcommand"
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

func (c *UpcomingCommand) Execute(ctx context.Context, cmdCtx *botcommand.CommandContext, params botcommand.UpcomingParams) error {
	options := parseUpcomingOptions(params)

	if params.Member != "" {
		if err := c.executeMemberUpcoming(ctx, cmdCtx.Room, params.Member, options.hours); err != nil {
			return fmt.Errorf("execute member upcoming: %w", err)
		}

		return nil
	}

	if err := c.executeAllUpcoming(ctx, cmdCtx.Room, options); err != nil {
		return fmt.Errorf("execute all upcoming: %w", err)
	}

	return nil
}

const upcomingHours = 24

type upcomingOptions struct {
	hours        int
	displayLimit int
}

func parseUpcomingOptions(params botcommand.UpcomingParams) upcomingOptions {
	displayLimit := normalizeUpcomingDisplayLimit(params.Limit, params.All)

	return upcomingOptions{
		hours:        upcomingHours,
		displayLimit: displayLimit,
	}
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
		return fmt.Errorf("find member: %w", err)
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

	return withCollabMembers(ctx, c.Deps().CollabMembers, named)
}

func (c *UpcomingCommand) replyDisplayNameFailure(ctx context.Context, roomID string, cause error) error {
	if ctx.Err() != nil {
		return fmt.Errorf("upcoming display names canceled: %w", ctx.Err())
	}

	c.Deps().Logger.Error("Failed to resolve upcoming display names", slog.Any("error", cause))

	if err := c.Deps().SendError(ctx, roomID, messaging.ErrUpcomingStreamQueryFailed); err != nil {
		return fmt.Errorf("send error: %w", err)
	}

	return nil
}

func (c *UpcomingCommand) CheckDependencies() error {
	if err := c.EnsureBaseDeps(); err != nil {
		return fmt.Errorf("ensure base dependencies: %w", err)
	}

	if c.Deps().Holodex == nil || c.Deps().Formatter == nil || c.Deps().Matcher == nil || c.Deps().CollabMembers == nil {
		return errors.New("upcoming command services not configured")
	}

	return nil
}
