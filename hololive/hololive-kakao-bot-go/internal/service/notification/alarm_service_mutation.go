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
	stdErrors "errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/valkey-io/valkey-go"
)

func (as *AlarmService) AddAlarm(ctx context.Context, req domain.AddAlarmRequest) (bool, error) {
	startedAt := time.Now()

	var opErr error

	defer func() {
		observeAlarmServiceOperation("add", startedAt, opErr)
	}()

	roomID := req.RoomID
	channelID := req.ChannelID

	alarmTypes := normalizedAlarmTypes(req.AlarmTypes)
	alarmKey := as.getAlarmKey(roomID)

	alreadyRegistered, err := as.cache.SIsMember(ctx, alarmKey, channelID)
	if err != nil {
		opErr = fmt.Errorf("check existing alarm: %w", err)
		as.logger.Error("Failed to check existing alarm", slog.Any("error", err))
		return false, opErr
	}

	if alreadyRegistered {
		return false, nil
	}

	record := buildAlarmRecord(req, alarmTypes)
	persistErr := as.persistAlarm(ctx, record)
	if persistErr != nil {
		opErr = fmt.Errorf("persist alarm before cache write: %w", persistErr)
		as.logger.Error("Failed to persist alarm before cache write", slog.Any("error", persistErr))
		return false, opErr
	}

	added, err := as.cacheAlarm(ctx, record)
	if err != nil {
		opErr = as.rebuildAlarmCacheFromRepository(ctx, "add", fmt.Errorf("add alarm: %w", err))
		as.logger.Error("Failed to add alarm", slog.Any("error", opErr))
		return false, fmt.Errorf("rebuild add cache from repository: %w", opErr)
	}

	as.logAlarmAdded(req, alarmTypes)

	if syncErr := as.syncPlatformMappingForChannel(ctx, channelID); syncErr != nil && as.logger != nil {
		as.logger.Warn("Failed to sync platform alarm mapping after add",
			slog.Any("error", syncErr),
			slog.String("channel_id", channelID),
			slog.String("room_id", roomID),
		)
	}

	return added > 0, nil
}

func (as *AlarmService) RemoveAlarm(ctx context.Context, roomID, channelID string, alarmTypes domain.AlarmTypes) (bool, error) {
	startedAt := time.Now()

	var opErr error

	defer func() {
		observeAlarmServiceOperation("remove", startedAt, opErr)
	}()

	exists, err := as.roomAlarmExists(ctx, roomID, channelID)
	if err != nil {
		opErr = err
		return false, opErr
	}

	if !exists {
		return false, nil
	}

	alarmTypes = normalizedRemovalAlarmTypes(alarmTypes)

	deleteErr := as.deleteAlarm(ctx, roomID, channelID)
	if deleteErr != nil {
		opErr = fmt.Errorf("delete alarm before cache removal: %w", deleteErr)
		as.logger.Error("Failed to persist alarm removal before cache write", slog.Any("error", deleteErr))
		return false, opErr
	}

	removed, err := as.removeAlarmFromCache(ctx, roomID, channelID, alarmTypes)
	if err != nil {
		opErr = as.rebuildAlarmCacheFromRepository(ctx, "remove", fmt.Errorf("remove alarm: %w", err))
		as.logger.Error("Failed to remove alarm", slog.Any("error", opErr))
		return false, fmt.Errorf("rebuild remove cache from repository: %w", opErr)
	}

	if syncErr := as.syncPlatformMappingForChannel(ctx, channelID); syncErr != nil && as.logger != nil {
		as.logger.Warn("Failed to sync platform alarm mapping after remove",
			slog.Any("error", syncErr),
			slog.String("channel_id", channelID),
			slog.String("room_id", roomID),
		)
	}

	as.logger.Info("Alarm removed",
		slog.String("room_id", roomID),
		slog.String("channel_id", channelID),
		slog.Any("alarm_types", alarmTypes),
	)

	if as.alarmRepo != nil {
		return true, nil
	}

	return removed, nil
}

func (as *AlarmService) cacheAlarm(ctx context.Context, record *domain.Alarm) (int64, error) {
	if record == nil {
		return 0, stdErrors.New("alarm is nil")
	}

	alarmKey := as.getAlarmKey(record.RoomID)
	added, err := as.cache.SAdd(ctx, alarmKey, []string{record.ChannelID})
	if err != nil {
		return 0, fmt.Errorf("add room alarm: %w", err)
	}

	registryKey := as.getRegistryKey(record.RoomID)
	if _, err := as.cache.SAdd(ctx, AlarmRegistryKey, []string{registryKey}); err != nil {
		return 0, fmt.Errorf("add room registry: %w", err)
	}

	builder := as.cache.Builder()
	saddCmds := make([]valkey.Completed, len(record.AlarmTypes))
	for i, alarmType := range record.AlarmTypes {
		subsKey := as.channelSubscribersKeyByType(record.ChannelID, alarmType)
		saddCmds[i] = builder.Sadd().Key(subsKey).Member(registryKey).Build()
	}

	results := as.cache.DoMulti(ctx, saddCmds...)
	if len(results) != len(saddCmds) {
		return 0, fmt.Errorf("add channel subscribers: unexpected result count: %d", len(results))
	}

	for i, result := range results {
		if err := result.Error(); err != nil {
			return 0, fmt.Errorf("add channel subscriber type %s: %w", record.AlarmTypes[i], err)
		}
	}

	if _, err := as.cache.SAdd(ctx, AlarmChannelRegistryKey, []string{record.ChannelID}); err != nil {
		return 0, fmt.Errorf("add channel registry: %w", err)
	}

	if err := as.CacheMemberName(ctx, record.ChannelID, record.MemberName); err != nil {
		return 0, fmt.Errorf("cache member name: %w", err)
	}

	if record.RoomName != "" {
		if err := as.cache.HSet(ctx, RoomNamesCacheKey, record.RoomID, record.RoomName); err != nil {
			return 0, fmt.Errorf("cache room name: %w", err)
		}
	}

	if record.UserName != "" && record.UserID != "" {
		if err := as.cache.HSet(ctx, UserNamesCacheKey, record.UserID, record.UserName); err != nil {
			return 0, fmt.Errorf("cache user name: %w", err)
		}
	}

	return added, nil
}

func (as *AlarmService) roomAlarmExists(ctx context.Context, roomID, channelID string) (bool, error) {
	if as.alarmRepo != nil {
		alarms, err := findRoomAlarmsFromRepository(ctx, as.alarmRepo, roomID)
		if err != nil {
			return false, fmt.Errorf("find room alarms: %w", err)
		}

		for _, alarm := range alarms {
			if alarm != nil && alarm.ChannelID == channelID {
				return true, nil
			}
		}

		return false, nil
	}

	exists, err := as.cache.SIsMember(ctx, as.getAlarmKey(roomID), channelID)
	if err != nil {
		return false, fmt.Errorf("check room alarm membership: %w", err)
	}

	return exists, nil
}

func (as *AlarmService) loadRoomAlarmsForMutation(ctx context.Context, roomID string) ([]*domain.Alarm, error) {
	if as.alarmRepo != nil {
		alarms, err := findRoomAlarmsFromRepository(ctx, as.alarmRepo, roomID)
		if err != nil {
			return nil, fmt.Errorf("find room alarms: %w", err)
		}

		return alarms, nil
	}

	channelIDs, err := as.GetRoomAlarms(ctx, roomID)
	if err != nil {
		return nil, fmt.Errorf("get room alarms: %w", err)
	}

	alarms := make([]*domain.Alarm, 0, len(channelIDs))
	for _, channelID := range channelIDs {
		alarms = append(alarms, &domain.Alarm{RoomID: roomID, ChannelID: channelID})
	}

	return alarms, nil
}

func uniqueAlarmChannelIDs(alarms []*domain.Alarm) []string {
	if len(alarms) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(alarms))
	channelIDs := make([]string, 0, len(alarms))
	for _, alarm := range alarms {
		if alarm == nil || alarm.ChannelID == "" {
			continue
		}
		if _, ok := seen[alarm.ChannelID]; ok {
			continue
		}

		seen[alarm.ChannelID] = struct{}{}
		channelIDs = append(channelIDs, alarm.ChannelID)
	}

	return channelIDs
}

func (as *AlarmService) removeAlarmFromCache(ctx context.Context, roomID, channelID string, alarmTypes domain.AlarmTypes) (bool, error) {
	alarmKey := as.getAlarmKey(roomID)
	removed, err := as.cache.SRem(ctx, alarmKey, []string{channelID})
	if err != nil {
		return false, fmt.Errorf("remove room alarm: %w", err)
	}

	registryKey := as.getRegistryKey(roomID)
	if err := as.removeChannelSubscribers(ctx, channelID, registryKey, alarmTypes); err != nil {
		return false, fmt.Errorf("remove channel subscribers: %w", err)
	}

	if err := as.cleanupChannelRegistryIfEmpty(ctx, channelID); err != nil {
		return false, fmt.Errorf("cleanup channel registry if empty: %w", err)
	}

	remainingAlarms, err := as.cache.SMembers(ctx, alarmKey)
	if err != nil {
		return false, fmt.Errorf("read remaining room alarms: %w", err)
	}

	if len(remainingAlarms) == 0 {
		if _, err := as.cache.SRem(ctx, AlarmRegistryKey, []string{registryKey}); err != nil {
			return false, fmt.Errorf("remove room registry: %w", err)
		}

		as.logger.Info("Room removed from registry (no alarms left)", slog.String("room_id", roomID))
	}

	return removed > 0, nil
}

func (as *AlarmService) clearRoomAlarmsFromCache(ctx context.Context, roomID string, channelIDs []string) (int, error) {
	if len(channelIDs) == 0 {
		return 0, nil
	}

	alarmKey := as.getAlarmKey(roomID)
	removed, err := as.cache.SRem(ctx, alarmKey, channelIDs)
	if err != nil {
		return 0, fmt.Errorf("remove room alarms: %w", err)
	}

	registryKey := as.getRegistryKey(roomID)
	if err := as.clearChannelSubscribersPipeline(ctx, channelIDs, registryKey); err != nil {
		return 0, fmt.Errorf("clear channel subscribers pipeline: %w", err)
	}

	if _, err := as.cache.SRem(ctx, AlarmRegistryKey, []string{registryKey}); err != nil {
		return 0, fmt.Errorf("remove room registry: %w", err)
	}

	return int(removed), nil
}

func (as *AlarmService) ClearRoomAlarms(ctx context.Context, roomID string) (int, error) {
	startedAt := time.Now()

	var opErr error

	defer func() {
		observeAlarmServiceOperation("clear", startedAt, opErr)
	}()

	alarmRecords, err := as.loadRoomAlarmsForMutation(ctx, roomID)
	if err != nil {
		opErr = err
		return 0, fmt.Errorf("load room alarms for mutation: %w", err)
	}

	if len(alarmRecords) == 0 {
		return 0, nil
	}

	deleteErr := as.deleteRoomAlarms(ctx, roomID)
	if deleteErr != nil {
		opErr = fmt.Errorf("delete room alarms before cache clear: %w", deleteErr)
		as.logger.Error("Failed to persist room alarm clear before cache write", slog.Any("error", deleteErr))
		return 0, opErr
	}

	channelIDs := uniqueAlarmChannelIDs(alarmRecords)
	removed, err := as.clearRoomAlarmsFromCache(ctx, roomID, channelIDs)
	if err != nil {
		opErr = as.rebuildAlarmCacheFromRepository(ctx, "clear", fmt.Errorf("clear room alarms: %w", err))
		as.logger.Error("Failed to clear room alarms", slog.Any("error", opErr))
		return 0, fmt.Errorf("rebuild clear cache from repository: %w", opErr)
	}

	for _, channelID := range channelIDs {
		if err := as.cleanupChannelRegistryIfEmpty(ctx, channelID); err != nil && as.logger != nil {
			as.logger.Warn("Failed to cleanup channel registry during room alarm clear",
				slog.String("room_id", roomID),
				slog.String("channel_id", channelID),
				slog.Any("error", err),
			)
		}

		if syncErr := as.syncPlatformMappingForChannel(ctx, channelID); syncErr != nil && as.logger != nil {
			as.logger.Warn("Failed to sync platform alarm mapping after clear",
				slog.Any("error", syncErr),
				slog.String("room_id", roomID),
				slog.String("channel_id", channelID),
			)
		}
	}

	as.logger.Info("All alarms cleared",
		slog.String("room_id", roomID),
		slog.Int("count", len(channelIDs)),
	)

	if as.alarmRepo != nil {
		return len(channelIDs), nil
	}

	return removed, nil
}
