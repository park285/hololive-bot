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
	"strings"
	"time"

	"github.com/park285/shared-go/v2/pkg/stringutil"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/bot/orchestration/transport"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/botcommand"
	handlercore "github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers/handlercore"
)

// 포스터의 주 범위는 일본 방송 기준이라 오늘 날짜를 JST로 정한다.
var (
	scheduleZone = time.FixedZone("JST", 9*60*60)
	scheduleNow  = time.Now
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
	if err := c.ensureDeps(); err != nil {
		return fmt.Errorf("ensure dependencies: %w", err)
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
		return fmt.Errorf("find member: %w", err)
	}

	if channel == nil {
		return nil
	}

	hours := days * 24

	streams, err := c.Deps().Holodex.GetChannelSchedule(ctx, channel.ID, hours, true)
	if err != nil {
		sent, posterErr := c.sendSchedulePoster(ctx, room, channel.ID)
		if posterErr != nil || sent {
			return posterErr
		}

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

		// 출연자를 뺀 일정으로 덮지 않고 일정 조회 실패로 알린다.
		c.Deps().Logger.Error("Failed to resolve schedule collab members", slog.Any("error", err))

		if sendErr := c.Deps().SendError(ctx, room, messaging.ErrScheduleQueryFailed); sendErr != nil {
			return fmt.Errorf("send error: %w", sendErr)
		}

		return nil
	}

	message := c.Deps().Formatter.ChannelSchedule(ctx, channel, streams, days)

	if sendErr := c.Deps().SendMessage(ctx, room, message); sendErr != nil {
		return fmt.Errorf("send message: %w", sendErr)
	}

	_, err = c.sendSchedulePoster(ctx, room, channel.ID)

	return err
}

// sendSchedulePoster는 포스터 조회·발송 실패를 일정 응답 실패로 만들지 않는다.
// 다만 Iris 전달 결과 불명은 재시도가 중복 발송이 될 수 있어 호출자에게 그대로 올린다.
func (c *ScheduleCommand) sendSchedulePoster(ctx context.Context, room, channelID string) (bool, error) {
	reader := c.Deps().SchedulePosters
	if reader == nil {
		return false, nil
	}

	now := scheduleNow().In(scheduleZone)
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)

	image, found, err := reader.PublishedImage(ctx, channelID, day)
	if err != nil {
		c.Deps().Logger.Warn("Failed to load schedule poster", slog.String("channel_id", channelID), slog.Any("error", err))

		return false, nil
	}

	if !found {
		return false, nil
	}

	err = c.Deps().SendImage(transport.WithImageContentType(ctx, image.MIME), room, image.Data)
	if transport.IsReplyOutcomeUnknown(err) {
		return false, fmt.Errorf("send schedule poster: %w", err)
	}

	if err != nil {
		c.Deps().Logger.Warn("Failed to send schedule poster", slog.String("channel_id", channelID), slog.Any("error", err))

		return false, nil
	}

	return true, nil
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

	message := stringutil.TrimSpace(cmdCtx.Message)
	if message == "" {
		return false
	}

	message = strings.TrimLeft(message, "!/\\.")
	message = stringutil.TrimSpace(message)

	normalizedMessage := stringutil.Normalize(message)

	return normalizedMessage == "멤버" || normalizedMessage == "member"
}
