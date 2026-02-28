package command

import (
	"context"
	"fmt"

	"github.com/kapu/hololive-shared/pkg/adapter"
	"github.com/kapu/hololive-shared/pkg/domain"
)

// UpcomingCommand: 예정된 방송 목록을 조회하는 커맨드 핸들러
type UpcomingCommand struct {
	BaseCommand
}

// NewUpcomingCommand: 예정 방송 조회 커맨드 핸들러를 생성합니다.
func NewUpcomingCommand(deps *Dependencies) *UpcomingCommand {
	return &UpcomingCommand{BaseCommand: NewBaseCommand(deps)}
}

// Name: 커맨드의 이름("upcoming")을 반환합니다.
func (c *UpcomingCommand) Name() string {
	return "upcoming"
}

// Description: 커맨드에 대한 설명을 반환합니다.
func (c *UpcomingCommand) Description() string {
	return "예정된 방송 목록"
}

// Execute: 예정된 방송 목록을 Holodex API로부터 조회하여 출력한다. (멤버 필터링 가능)
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
	showAll, _ := params["all"].(bool)
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
	channel, err := FindActiveMemberOrError(ctx, c.Deps(), roomID, memberName)
	if err != nil {
		return fmt.Errorf("failed to find member %q: %w", memberName, err)
	}

	streams, err := c.Deps().Holodex.GetUpcomingStreams(ctx, hours)
	if err != nil {
		return c.Deps().SendError(ctx, roomID, adapter.ErrUpcomingStreamQueryFailed)
	}

	memberStreams := make([]*domain.Stream, 0, len(streams))
	for _, stream := range streams {
		if stream.ChannelID == channel.ID {
			memberStreams = append(memberStreams, stream)
		}
	}

	if len(memberStreams) == 0 {
		return c.Deps().SendMessage(ctx, roomID, c.Deps().Formatter.FormatMemberNoUpcoming(channel.Name, hours))
	}

	message := c.Deps().Formatter.UpcomingStreams(ctx, memberStreams, hours)
	return c.Deps().SendMessage(ctx, roomID, message)
}

func (c *UpcomingCommand) executeAllUpcoming(ctx context.Context, roomID string, options upcomingOptions) error {
	streams, err := c.Deps().Holodex.GetUpcomingStreams(ctx, options.hours)
	if err != nil {
		return c.Deps().SendError(ctx, roomID, adapter.ErrUpcomingStreamQueryFailed)
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
		return fmt.Errorf("upcoming command services not configured")
	}

	return nil
}
