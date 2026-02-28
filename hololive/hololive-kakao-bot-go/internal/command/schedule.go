package command

import (
	"context"
	"fmt"
	"strings"

	"github.com/kapu/hololive-shared/pkg/adapter"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/stringutil"
)

// ScheduleCommand: 특정 멤버의 향후 방송 일정을 조회하여 보여주는 명령어
type ScheduleCommand struct {
	BaseCommand
}

// NewScheduleCommand: NewScheduleCommand 인스턴스를 생성합니다.
func NewScheduleCommand(deps *Dependencies) *ScheduleCommand {
	return &ScheduleCommand{BaseCommand: NewBaseCommand(deps)}
}

// Name: 명령어의 고유 식별자('schedule')를 반환합니다.
func (c *ScheduleCommand) Name() string {
	return "schedule"
}

// Description: 명령어에 대한 사용자용 설명('특정 멤버 일정 조회')을 반환합니다.
func (c *ScheduleCommand) Description() string {
	return "특정 멤버 일정 조회"
}

// Execute: 특정 멤버의 방송 일정을 조회하고, 결과를 포맷팅하여 채팅방에 전송합니다.
func (c *ScheduleCommand) Execute(ctx context.Context, cmdCtx *domain.CommandContext, params map[string]any) error {
	if err := c.ensureDeps(); err != nil {
		return fmt.Errorf("failed to ensure dependencies: %w", err)
	}

	rawCommandToken, _ := params["_raw_command"].(string)
	delete(params, "_raw_command")

	memberName, hasMember := params["member"].(string)
	if !hasMember || memberName == "" {
		if shouldSuppressSchedulePrompt(cmdCtx, rawCommandToken) {
			return nil
		}
		return c.Deps().SendError(ctx, cmdCtx.Room, adapter.ErrScheduleNeedMemberName)
	}
	days := 7
	if d, ok := params["days"]; ok {
		switch v := d.(type) {
		case float64:
			days = int(v)
		case int:
			days = v
		}
	}

	if days < 1 {
		days = 7
	}
	if days > 30 {
		days = 30
	}

	channel, err := FindActiveMemberOrError(ctx, c.Deps(), cmdCtx.Room, memberName)
	if err != nil {
		return fmt.Errorf("failed to find member %q: %w", memberName, err)
	}

	hours := days * 24
	streams, err := c.Deps().Holodex.GetChannelSchedule(ctx, channel.ID, hours, true)
	if err != nil {
		return c.Deps().SendError(ctx, cmdCtx.Room, adapter.ErrScheduleQueryFailed)
	}

	message := c.Deps().Formatter.ChannelSchedule(ctx, channel, streams, days)
	return c.Deps().SendMessage(ctx, cmdCtx.Room, message)
}

func (c *ScheduleCommand) ensureDeps() error {
	if err := c.EnsureBaseDeps(); err != nil {
		return fmt.Errorf("failed to ensure base dependencies: %w", err)
	}

	if c.Deps().Matcher == nil || c.Deps().Holodex == nil || c.Deps().Formatter == nil {
		return fmt.Errorf("schedule command services not configured")
	}

	return nil
}

func shouldSuppressSchedulePrompt(cmdCtx *domain.CommandContext, rawToken string) bool {
	if normalized := stringutil.Normalize(rawToken); normalized == "멤버" || normalized == "member" {
		return true
	}

	if cmdCtx == nil {
		return false
	}

	message := stringutil.TrimSpace(cmdCtx.Message)
	if message == "" {
		return false
	}

	// remove common command prefixes
	message = strings.TrimLeft(message, "!/\\.")
	message = stringutil.TrimSpace(message)

	normalizedMessage := stringutil.Normalize(message)
	return normalizedMessage == "멤버" || normalizedMessage == "member"
}
