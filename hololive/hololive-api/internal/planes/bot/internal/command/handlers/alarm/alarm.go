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

package alarm

import (
	"context"
	stdErrors "errors"
	"fmt"
	"log/slog"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging/formatter"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/botcommand"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers/handlercore"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/domain/mekparkhost"
	sharedprivacylog "github.com/kapu/hololive-shared/pkg/privacylog"
)

type AlarmCommand struct {
	handlercore.BaseCommand
}

type alarmTarget struct {
	ChannelID string
	HostID    string
	Name      string
}

type alarmActionHandler func(context.Context, *botcommand.CommandContext, map[string]any) error

func NewAlarmCommand(deps *handlercore.Dependencies) *AlarmCommand {
	return &AlarmCommand{BaseCommand: handlercore.NewBaseCommand(deps)}
}

func (c *AlarmCommand) Name() string {
	return "alarm"
}

func (c *AlarmCommand) Description() string {
	return "방송 알람 관리"
}

func (c *AlarmCommand) Execute(ctx context.Context, cmdCtx *botcommand.CommandContext, params map[string]any) error {
	if err := c.executeAction(ctx, cmdCtx, params, alarmAction(params)); err != nil {
		return fmt.Errorf("execute action: %w", err)
	}

	return nil
}

func alarmAction(params map[string]any) string {
	action, hasAction := params["action"].(string)
	if !hasAction {
		return "list"
	}

	return action
}

func (c *AlarmCommand) executeAction(ctx context.Context, cmdCtx *botcommand.CommandContext, params map[string]any, action string) error {
	handler, ok := c.alarmActionHandlers()[action]
	if !ok {
		if err := c.Deps().SendMessage(ctx, cmdCtx.Room, c.Deps().Formatter.FormatHelp(ctx)); err != nil {
			return fmt.Errorf("send message: %w", err)
		}

		return nil
	}

	if err := handler(ctx, cmdCtx, params); err != nil {
		return fmt.Errorf("handler: %w", err)
	}

	return nil
}

func (c *AlarmCommand) alarmActionHandlers() map[string]alarmActionHandler {
	return map[string]alarmActionHandler{
		"set":     c.handleAdd,
		"add":     c.handleAdd,
		"remove":  c.handleRemove,
		"delete":  c.handleRemove,
		"list":    c.handleListAction,
		"clear":   c.handleClearAction,
		"invalid": c.handleInvalid,
	}
}

func (c *AlarmCommand) handleListAction(ctx context.Context, cmdCtx *botcommand.CommandContext, _ map[string]any) error {
	c.Deps().Logger.Debug("Alarm list requested")

	if err := c.handleList(ctx, cmdCtx); err != nil {
		return fmt.Errorf("handle list: %w", err)
	}

	return nil
}

func (c *AlarmCommand) handleClearAction(ctx context.Context, cmdCtx *botcommand.CommandContext, _ map[string]any) error {
	if err := c.handleClear(ctx, cmdCtx); err != nil {
		return fmt.Errorf("handle clear: %w", err)
	}

	return nil
}

func (c *AlarmCommand) handleInvalid(ctx context.Context, cmdCtx *botcommand.CommandContext, _ map[string]any) error {
	c.Deps().Logger.Info("Invalid alarm command received",
		sharedprivacylog.RoomIDAttr(cmdCtx.Room),
	)

	if err := c.Deps().SendError(ctx, cmdCtx.Room, messaging.ErrInvalidAlarmUsage); err != nil {
		return fmt.Errorf("send error: %w", err)
	}

	return nil
}

func (c *AlarmCommand) CheckDependencies() error {
	if err := c.EnsureBaseDeps(); err != nil {
		return fmt.Errorf("ensure base dependencies: %w", err)
	}

	if c.Deps().Alarm == nil || c.Deps().Matcher == nil || c.Deps().Formatter == nil {
		return stdErrors.New("alarm command services not configured")
	}

	return nil
}

type alarmMutation struct {
	needMemberNameMessage string
	requestedLog          string
	resolve               func(ctx context.Context, room, memberName string) (*alarmTarget, error)
	apply                 func(ctx context.Context, cmdCtx *botcommand.CommandContext, channel *alarmTarget, alarmTypes domain.AlarmTypes) error
}

func (c *AlarmCommand) handleAdd(ctx context.Context, cmdCtx *botcommand.CommandContext, params map[string]any) error {
	return c.handleAlarmMutation(ctx, cmdCtx, params, alarmMutation{
		needMemberNameMessage: messaging.ErrAlarmNeedMemberNameAdd,
		requestedLog:          "Alarm add requested",
		resolve:               c.resolveAlarmAddMember,
		apply:                 c.addAlarmAndReply,
	})
}

func (c *AlarmCommand) handleRemove(ctx context.Context, cmdCtx *botcommand.CommandContext, params map[string]any) error {
	return c.handleAlarmMutation(ctx, cmdCtx, params, alarmMutation{
		needMemberNameMessage: messaging.ErrAlarmNeedMemberNameRemove,
		requestedLog:          "Alarm remove requested",
		resolve:               c.resolveAlarmMember,
		apply:                 c.removeAlarmAndReply,
	})
}

func (c *AlarmCommand) handleAlarmMutation(ctx context.Context, cmdCtx *botcommand.CommandContext, params map[string]any, mutation alarmMutation) error {
	memberName, err := c.requiredAlarmMemberName(ctx, cmdCtx.Room, params, mutation.needMemberNameMessage)
	if err != nil {
		return fmt.Errorf("require alarm member name: %w", err)
	}

	if memberName == "" {
		return nil
	}

	alarmTypes := c.parseAlarmTypes(params)

	// 운영 기본 레벨(info)에서 알람 변경 명령마다 room 가명을 계산해 버리지 않게 먼저 확인한다.
	if logger := c.Deps().Logger; logger.Enabled(ctx, slog.LevelDebug) {
		logger.Debug(mutation.requestedLog,
			sharedprivacylog.RoomIDAttr(cmdCtx.Room),
			slog.Any("types", alarmTypes))
	}

	channel, err := mutation.resolve(ctx, cmdCtx.Room, memberName)
	if stdErrors.Is(err, handlercore.ErrMemberLookupHandled) {
		return nil
	}

	if err != nil {
		return fmt.Errorf("resolve alarm member: %w", err)
	}

	if channel == nil {
		return nil
	}

	if err := mutation.apply(ctx, cmdCtx, channel, alarmTypes); err != nil {
		return fmt.Errorf("apply alarm mutation: %w", err)
	}

	return nil
}

func (c *AlarmCommand) requiredAlarmMemberName(ctx context.Context, room string, params map[string]any, message string) (string, error) {
	memberName, hasMember := params["member"].(string)
	if hasMember && memberName != "" {
		return memberName, nil
	}

	if err := c.Deps().SendError(ctx, room, message); err != nil {
		return "", fmt.Errorf("send error: %w", err)
	}

	return "", nil
}

func (c *AlarmCommand) resolveAlarmAddMember(ctx context.Context, room, memberName string) (*alarmTarget, error) {
	channel, err := c.resolveAlarmMember(ctx, room, memberName)
	if err != nil {
		return nil, fmt.Errorf("%w", err)
	}

	if channel == nil {
		return nil, handlercore.ErrMemberLookupHandled
	}

	blocked, err := handlercore.BlockGraduatedMember(ctx, c.Deps(), room, channel.ChannelID)
	if err != nil {
		return nil, fmt.Errorf("block graduated member: %w", err)
	}

	if blocked {
		return nil, handlercore.ErrMemberLookupHandled
	}

	return channel, nil
}

func (c *AlarmCommand) addAlarmAndReply(ctx context.Context, cmdCtx *botcommand.CommandContext, channel *alarmTarget, alarmTypes domain.AlarmTypes) error {
	added, err := c.Deps().Alarm.AddAlarm(ctx, &domain.AddAlarmRequest{
		RoomID:     cmdCtx.Room,
		ChannelID:  channel.ChannelID,
		HostID:     channel.HostID,
		AlarmTypes: alarmTypes,
	})
	if err != nil {
		c.Deps().Logger.Error("Failed to add alarm",
			slog.String("channel", channel.Name),
			slog.Any("error", err),
		)

		if sendErr := c.Deps().SendError(ctx, cmdCtx.Room, messaging.ErrAlarmAddFailed); sendErr != nil {
			return fmt.Errorf("send error: %w", sendErr)
		}

		return nil
	}

	message := c.Deps().Formatter.FormatAlarmAdded(ctx, channel.Name, added)

	if err := c.Deps().SendMessage(ctx, cmdCtx.Room, message); err != nil {
		return fmt.Errorf("send message: %w", err)
	}

	return nil
}

// 사용자-facing 응답을 이미 보낸 경우 handlercore.ErrMemberLookupHandled를 반환한다. 구독 전용 host를 matcher보다 먼저 찾는다.
func (c *AlarmCommand) resolveAlarmMember(ctx context.Context, room, memberName string) (*alarmTarget, error) {
	if channelID, host, ok := mekparkhost.FindSubscriptionMember(memberName); ok {
		return &alarmTarget{ChannelID: channelID, HostID: host.ID, Name: host.Name}, nil
	}

	// 동명이인 안내 예시는 삭제 명령에서도 "알람 추가"다.
	channel, err := handlercore.FindMemberWithCandidatesOrError(ctx, c.Deps(), room, memberName, "알람 추가")
	if err != nil {
		return nil, fmt.Errorf("find alarm member: %w", err)
	}

	return &alarmTarget{ChannelID: channel.ID, Name: channel.Name}, nil
}

func (c *AlarmCommand) removeAlarmAndReply(ctx context.Context, cmdCtx *botcommand.CommandContext, channel *alarmTarget, alarmTypes domain.AlarmTypes) error {
	var (
		removed bool
		err     error
	)

	if channel.HostID == "" {
		removed, err = c.Deps().Alarm.RemoveAlarm(ctx, cmdCtx.Room, channel.ChannelID, alarmTypes)
	} else {
		removed, err = c.Deps().Alarm.RemoveHostAlarm(ctx, cmdCtx.Room, channel.ChannelID, channel.HostID, alarmTypes)
	}

	if err != nil {
		c.Deps().Logger.Error("Failed to remove alarm",
			slog.String("channel", channel.Name),
			slog.Any("error", err),
		)

		if err := c.Deps().SendError(ctx, cmdCtx.Room, messaging.ErrAlarmRemoveFailed); err != nil {
			return fmt.Errorf("send error: %w", err)
		}

		return nil
	}

	message := c.Deps().Formatter.FormatAlarmRemoved(ctx, channel.Name, removed)

	if err := c.Deps().SendMessage(ctx, cmdCtx.Room, message); err != nil {
		return fmt.Errorf("send message: %w", err)
	}

	return nil
}

func (c *AlarmCommand) handleList(ctx context.Context, cmdCtx *botcommand.CommandContext) error {
	entries, err := c.Deps().Alarm.ListRoomAlarmsView(ctx, cmdCtx.Room)
	if err != nil {
		if err := c.Deps().SendError(ctx, cmdCtx.Room, messaging.ErrAlarmListFailed); err != nil {
			return fmt.Errorf("send error: %w", err)
		}

		return nil
	}

	alarmInfos := make([]formatter.AlarmListEntry, 0, len(entries))
	for _, entry := range entries {
		alarmInfos = append(alarmInfos, formatter.AlarmListEntry{
			MemberName: entry.MemberName,
			AlarmTypes: entry.AlarmTypes,
		})
	}

	message := c.Deps().Formatter.FormatAlarmList(ctx, alarmInfos)

	if err := c.Deps().SendMessage(ctx, cmdCtx.Room, message); err != nil {
		return fmt.Errorf("send message: %w", err)
	}

	return nil
}

func (c *AlarmCommand) handleClear(ctx context.Context, cmdCtx *botcommand.CommandContext) error {
	count, err := c.Deps().Alarm.ClearRoomAlarms(ctx, cmdCtx.Room)
	if err != nil {
		c.Deps().Logger.Error("Failed to clear alarms", slog.Any("error", err))

		if err := c.Deps().SendError(ctx, cmdCtx.Room, messaging.ErrAlarmClearFailed); err != nil {
			return fmt.Errorf("send error: %w", err)
		}

		return nil
	}

	message := c.Deps().Formatter.FormatAlarmCleared(ctx, count)

	if err := c.Deps().SendMessage(ctx, cmdCtx.Room, message); err != nil {
		return fmt.Errorf("send message: %w", err)
	}

	return nil
}

func (c *AlarmCommand) parseAlarmTypes(params map[string]any) domain.AlarmTypes {
	typeStr, hasType := params["type"].(string)
	if !hasType || typeStr == "" {
		return domain.DefaultAlarmTypes
	}

	if alarmTypes, ok := alarmTypesByName[typeStr]; ok {
		return alarmTypes
	}

	return domain.DefaultAlarmTypes
}

var alarmTypesByName = map[string]domain.AlarmTypes{
	"방송":        {domain.AlarmTypeLive},
	"라이브":       {domain.AlarmTypeLive},
	"live":      {domain.AlarmTypeLive},
	"커뮤니티":      {domain.AlarmTypeCommunity},
	"community": {domain.AlarmTypeCommunity},
	"쇼츠":        {domain.AlarmTypeShorts},
	"shorts":    {domain.AlarmTypeShorts},
	"전체":        domain.AllAlarmTypes,
	"all":       domain.AllAlarmTypes,
}
