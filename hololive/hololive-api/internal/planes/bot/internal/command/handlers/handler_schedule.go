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
	"strings"

	"github.com/park285/shared-go/v2/pkg/stringutil"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging"
	handlercore "github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers/handlercore"
	"github.com/kapu/hololive-shared/pkg/domain"
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

func (c *ScheduleCommand) Execute(ctx context.Context, cmdCtx *domain.CommandContext, params map[string]any) error {
	if err := c.ensureDeps(); err != nil {
		return fmt.Errorf("failed to ensure dependencies: %w", err)
	}

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
		return fmt.Errorf("failed to find member: %w", err)
	}

	if channel == nil {
		return nil
	}

	hours := days * 24

	streams, err := c.Deps().Holodex.GetChannelSchedule(ctx, channel.ID, hours, true)
	if err != nil {
		if err := c.Deps().SendError(ctx, room, messaging.ErrScheduleQueryFailed); err != nil {
			return fmt.Errorf("send error: %w", err)
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
	return clampScheduleDays(rawScheduleDays(params))
}

func rawScheduleDays(params map[string]any) int {
	d, ok := params["days"]
	if !ok {
		return 7
	}

	switch v := d.(type) {
	case float64:
		return int(v)
	case int:
		return v
	default:
		return 7
	}
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

func (c *ScheduleCommand) ensureDeps() error {
	if err := c.EnsureBaseDeps(); err != nil {
		return fmt.Errorf("failed to ensure base dependencies: %w", err)
	}

	if c.Deps().Matcher == nil || c.Deps().Holodex == nil || c.Deps().Formatter == nil {
		return errors.New("schedule command services not configured")
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

	message = strings.TrimLeft(message, "!/\\.")
	message = stringutil.TrimSpace(message)

	normalizedMessage := stringutil.Normalize(message)

	return normalizedMessage == "멤버" || normalizedMessage == "member"
}
