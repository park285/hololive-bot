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
	"fmt"
	"log/slog"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging/formatter"
	"github.com/kapu/hololive-shared/pkg/domain"
)

func (c *AlarmCommand) removeAlarmAndReply(ctx context.Context, cmdCtx *domain.CommandContext, channel *domain.Channel, alarmTypes domain.AlarmTypes) error {
	removed, err := c.Deps().Alarm.RemoveAlarm(ctx, cmdCtx.Room, channel.ID, alarmTypes)
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

func (c *AlarmCommand) handleList(ctx context.Context, cmdCtx *domain.CommandContext) error {
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
			NextStream: entry.NextStream,
		})
	}

	message := c.Deps().Formatter.FormatAlarmList(ctx, alarmInfos)

	if err := c.Deps().SendMessage(ctx, cmdCtx.Room, message); err != nil {
		return fmt.Errorf("send message: %w", err)
	}

	return nil
}

func (c *AlarmCommand) handleClear(ctx context.Context, cmdCtx *domain.CommandContext) error {
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
