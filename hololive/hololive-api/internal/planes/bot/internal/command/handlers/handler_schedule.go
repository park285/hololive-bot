package handlers

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/park285/shared-go/core/stringutil"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/botcommand"
	handlercore "github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers/handlercore"
)

type ScheduleCommand struct {
	handlercore.BaseCommand
}

func NewScheduleCommand(deps *handlercore.Dependencies) *ScheduleCommand {
	return &ScheduleCommand{BaseCommand: handlercore.NewBaseCommand(deps)}
}

func (c *ScheduleCommand) Name() string {
	return "schedule"
}

func (c *ScheduleCommand) Description() string {
	return "특정 멤버 일정 조회"
}

func (c *ScheduleCommand) Execute(ctx context.Context, cmdCtx *botcommand.CommandContext, params map[string]any) error {
	rawCommandToken := popRawScheduleCommandToken(params)
	memberName, ok := scheduleMemberName(params)

	if !ok {
		if shouldSuppressSchedulePrompt(cmdCtx, rawCommandToken) {
			return nil
		}

		if err := c.Deps().SendError(ctx, cmdCtx.Room, messaging.ErrScheduleNeedMemberName); err != nil {
			return fmt.Errorf("send error: %w", err)
		}

		return nil
	}

	days := scheduleDays(params)
	if err := c.executeMemberSchedule(ctx, cmdCtx.Room, memberName, days); err != nil {
		return fmt.Errorf("execute member schedule: %w", err)
	}

	return nil
}

func (c *ScheduleCommand) executeMemberSchedule(ctx context.Context, room, memberName string, days int) error {
	channel, err := handlercore.FindActiveMemberWithCandidatesOrError(ctx, c.Deps(), room, memberName, "일정")

	if memberLookupHandled(err) {
		return nil
	}

	if err != nil {
		return fmt.Errorf("find member: %w", err)
	}

	if channel == nil {
		return nil
	}

	hours := days * 24

	streams, err := c.Deps().Holodex.GetChannelSchedule(ctx, channel.ID, hours, true)
	if err != nil {
		if sendErr := c.Deps().SendError(ctx, room, messaging.ErrScheduleQueryFailed); sendErr != nil {
			return fmt.Errorf("send error: %w", sendErr)
		}

		return nil
	}

	streams, err = withCollabMembers(ctx, c.Deps().CollabMembers, streams)
	if err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("collab members query canceled: %w", ctx.Err())
		}

		c.Deps().Logger.Error("Failed to resolve schedule collab members", slog.Any("error", err))

		if sendErr := c.Deps().SendError(ctx, room, messaging.ErrScheduleQueryFailed); sendErr != nil {
			return fmt.Errorf("send error: %w", sendErr)
		}

		return nil
	}

	message := c.Deps().Formatter.ChannelSchedule(ctx, channel, streams, days)

	if err := c.Deps().SendMessage(ctx, room, message); err != nil {
		return fmt.Errorf("send message: %w", err)
	}

	return nil
}

func popRawScheduleCommandToken(params map[string]any) string {
	rawCommandToken := stringParam(params, "_raw_command")
	delete(params, "_raw_command")

	return rawCommandToken
}

func scheduleMemberName(params map[string]any) (string, bool) {
	memberName, ok := params[paramMember].(string)
	if !ok || memberName == "" {
		return "", false
	}

	return memberName, true
}

func scheduleDays(params map[string]any) int {
	return clampScheduleDays(intParam(params, paramDays, 7))
}

func clampScheduleDays(days int) int {
	if days < 1 {
		return 7
	}

	if days > 30 {
		return 30
	}

	return days
}

func (c *ScheduleCommand) CheckDependencies() error {
	if err := c.EnsureBaseDeps(); err != nil {
		return fmt.Errorf("ensure base dependencies: %w", err)
	}

	if c.Deps().Matcher == nil || c.Deps().Holodex == nil || c.Deps().Formatter == nil || c.Deps().CollabMembers == nil {
		return errors.New("schedule command services not configured")
	}

	return nil
}

func shouldSuppressSchedulePrompt(cmdCtx *botcommand.CommandContext, rawToken string) bool {
	if normalized := stringutil.Normalize(rawToken); normalized == "멤버" || normalized == "member" {
		return true
	}

	if cmdCtx == nil {
		return false
	}

	message := strings.TrimSpace(cmdCtx.Message)
	if message == "" {
		return false
	}

	message = strings.TrimLeft(message, "!/\\.")
	message = strings.TrimSpace(message)

	normalizedMessage := stringutil.Normalize(message)

	return normalizedMessage == "멤버" || normalizedMessage == "member"
}
