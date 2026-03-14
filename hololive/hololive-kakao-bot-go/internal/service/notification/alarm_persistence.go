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

package notification

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func (as *AlarmService) submitPersistTask(action, roomID string, task func()) {
	if as.persistExecutor == nil {
		if as.logger != nil {
			as.logger.Error("Persist executor is not initialized",
				slog.String("action", action),
				slog.String("room_id", roomID),
			)
		}
		return
	}

	if err := as.persistExecutor.Submit(roomID, task); err != nil {
		if as.logger != nil {
			as.logger.Warn("Failed to submit persist task to executor",
				slog.String("action", action),
				slog.String("room_id", roomID),
				slog.Any("error", err),
			)
		}
	}
}

// persistAlarmAsync: 알람을 DB에 비동기로 저장한다. (Write-Through)
// 사용자 응답을 지연시키지 않기 위해 goroutine으로 실행한다.
//
//nolint:contextcheck // Async 작업은 caller context와 독립적으로 실행되어야 함
func (as *AlarmService) persistAlarmAsync(alarm *domain.Alarm) {
	if as.alarmWriter == nil || alarm == nil {
		return
	}

	as.submitPersistTask("persist_alarm", alarm.RoomID, func() {
		ctx, cancel := context.WithTimeout(context.Background(), alarmPersistTaskTimeout)
		defer cancel()

		if err := as.alarmWriter.Add(ctx, alarm); err != nil {
			as.logger.Warn("Failed to persist alarm to DB (async)",
				slog.String("room_id", alarm.RoomID),
				slog.String("channel_id", alarm.ChannelID),
				slog.Any("error", err),
			)
		}
	})
}

// removeAlarmAsync: 알람을 DB에서 비동기로 삭제한다. (Write-Through, 방 기반)
//
//nolint:contextcheck // Async 작업은 caller context와 독립적으로 실행되어야 함
func (as *AlarmService) removeAlarmAsync(roomID, channelID string) {
	if as.alarmWriter == nil {
		return
	}

	as.submitPersistTask("remove_alarm", roomID, func() {
		ctx, cancel := context.WithTimeout(context.Background(), alarmPersistTaskTimeout)
		defer cancel()

		if err := as.alarmWriter.Remove(ctx, roomID, channelID); err != nil {
			as.logger.Warn("Failed to remove alarm from DB (async)",
				slog.String("room_id", roomID),
				slog.String("channel_id", channelID),
				slog.Any("error", err),
			)
		}
	})
}

// clearRoomAlarmsAsync: 방의 모든 알람을 DB에서 비동기로 삭제한다. (Write-Through)
//
//nolint:contextcheck // Async 작업은 caller context와 독립적으로 실행되어야 함
func (as *AlarmService) clearRoomAlarmsAsync(roomID string) {
	if as.alarmWriter == nil {
		return
	}

	as.submitPersistTask("clear_room_alarms", roomID, func() {
		ctx, cancel := context.WithTimeout(context.Background(), alarmPersistTaskTimeout)
		defer cancel()

		if _, err := as.alarmWriter.ClearByRoom(ctx, roomID); err != nil {
			as.logger.Warn("Failed to clear room alarms from DB (async)",
				slog.String("room_id", roomID),
				slog.Any("error", err),
			)
		}
	})
}

// WarmCacheFromDB: 앱 시작 시 DB에서 모든 알람을 로드하여 Valkey 캐시를 워밍합니다.
// 이 메서드는 앱 시작 시 한 번만 호출되며, 이후 런타임 중에는 Valkey만 사용한다.
func (as *AlarmService) WarmCacheFromDB(ctx context.Context) error {
	if as.alarmRepo == nil {
		as.logger.Info("Alarm repository not configured, skipping cache warming")
		return nil
	}

	alarms, err := as.alarmRepo.LoadAll(ctx)
	if err != nil {
		return fmt.Errorf("load alarms from DB: %w", err)
	}

	if len(alarms) == 0 {
		as.logger.Info("No alarms found in DB, cache warming skipped")
		return nil
	}

	for _, a := range alarms {
		// 방 기반 키: alarm:{roomID}
		alarmKey := as.getAlarmKey(a.RoomID)
		registryKey := a.RegistryKey()
		channelSubsKey := as.channelSubscribersKey(a.ChannelID)

		if _, err := as.cache.SAdd(ctx, alarmKey, []string{a.ChannelID}); err != nil {
			as.logger.Warn("Failed to warm alarm cache",
				slog.String("alarm_key", alarmKey),
				slog.Any("error", err),
			)
		}

		_, _ = as.cache.SAdd(ctx, AlarmRegistryKey, []string{registryKey})
		_, _ = as.cache.SAdd(ctx, channelSubsKey, []string{registryKey})
		_, _ = as.cache.SAdd(ctx, AlarmChannelRegistryKey, []string{a.ChannelID})

		for _, alarmType := range a.AlarmTypes {
			typeKey := as.channelSubscribersKeyByType(a.ChannelID, alarmType)
			if typeKey != channelSubsKey {
				_, _ = as.cache.SAdd(ctx, typeKey, []string{registryKey})
			}
		}

		if a.MemberName != "" {
			_ = as.CacheMemberName(ctx, a.ChannelID, a.MemberName)
		}

		if a.RoomName != "" {
			_ = as.cache.HSet(ctx, RoomNamesCacheKey, a.RoomID, a.RoomName)
		}
		if a.UserName != "" {
			_ = as.cache.HSet(ctx, UserNamesCacheKey, a.UserID, a.UserName)
		}
	}

	as.logger.Info("Cache warmed from DB",
		slog.Int("alarms_loaded", len(alarms)),
	)

	return nil
}
