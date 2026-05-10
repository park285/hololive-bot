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
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	sharedalarmkeys "github.com/kapu/hololive-shared/pkg/service/alarm/keys"
	"github.com/valkey-io/valkey-go"
)

func (as *AlarmService) AddAlarm(ctx context.Context, req domain.AddAlarmRequest) (bool, error) {
	startedAt := time.Now()

	var opErr error

	defer func() {
		observeAlarmServiceOperation("add", startedAt, opErr)
	}()

	req.RoomID = strings.TrimSpace(req.RoomID)
	req.UserID = strings.TrimSpace(req.UserID)
	req.ChannelID = strings.TrimSpace(req.ChannelID)
	req.MemberName = strings.TrimSpace(req.MemberName)
	req.RoomName = strings.TrimSpace(req.RoomName)
	req.UserName = strings.TrimSpace(req.UserName)

	if req.RoomID == "" || req.ChannelID == "" {
		opErr = fmt.Errorf("room_id and channel_id are required")
		return false, opErr
	}

	requestedTypes, err := normalizeAlarmTypesStrict(req.AlarmTypes, domain.DefaultAlarmTypes)
	if err != nil {
		opErr = err
		return false, err
	}

	existing, err := as.findAlarmRecordForMutation(ctx, req.RoomID, req.ChannelID)
	if err != nil {
		opErr = err
		return false, err
	}

	mergedTypes := requestedTypes
	newlyAddedTypes := requestedTypes

	if existing != nil {
		existingTypes, err := normalizeAlarmTypesStrict(existing.AlarmTypes, domain.DefaultAlarmTypes)
		if err != nil {
			opErr = err
			return false, err
		}

		mergedTypes = mergeAlarmTypes(existingTypes, requestedTypes)
		newlyAddedTypes = subtractAlarmTypes(mergedTypes, existingTypes)
		if len(newlyAddedTypes) == 0 {
			return false, nil
		}
	}

	record := buildAlarmRecord(req, mergedTypes)

	var persistErr error
	if existing != nil {
		persistErr = as.updateAlarmTypes(ctx, record)
	} else {
		persistErr = as.persistAlarm(ctx, record)
	}
	if persistErr != nil {
		opErr = fmt.Errorf("persist alarm before cache write: %w", persistErr)
		if as.logger != nil {
			as.logger.Error("Failed to persist alarm before cache write", slog.Any("error", persistErr))
		}
		return false, opErr
	}

	cacheRecord := *record
	if existing != nil {
		cacheRecord.AlarmTypes = newlyAddedTypes
	}

	added, err := as.cacheAlarm(ctx, &cacheRecord)
	if err != nil {
		opErr = as.rebuildAlarmCacheFromRepository(ctx, "add", fmt.Errorf("add alarm: %w", err))
		if as.logger != nil {
			as.logger.Error("Failed to add alarm", slog.Any("error", opErr))
		}
		return false, fmt.Errorf("rebuild add cache from repository: %w", opErr)
	}

	as.logAlarmAdded(req, newlyAddedTypes)

	if syncErr := as.syncPlatformMappingForChannel(ctx, req.ChannelID); syncErr != nil && as.logger != nil {
		as.logger.Warn("Failed to sync platform alarm mapping after add",
			slog.Any("error", syncErr),
			slog.String("channel_id", req.ChannelID),
			slog.String("room_id", req.RoomID),
		)
	}

	return added > 0 || existing != nil, nil
}

func (as *AlarmService) RemoveAlarm(ctx context.Context, roomID, channelID string, alarmTypes domain.AlarmTypes) (bool, error) {
	startedAt := time.Now()

	var opErr error

	defer func() {
		observeAlarmServiceOperation("remove", startedAt, opErr)
	}()

	roomID = strings.TrimSpace(roomID)
	channelID = strings.TrimSpace(channelID)
	if roomID == "" || channelID == "" {
		opErr = fmt.Errorf("room_id and channel_id are required")
		return false, opErr
	}

	requestedRemovalTypes, err := normalizeAlarmTypesStrict(alarmTypes, domain.AllAlarmTypes)
	if err != nil {
		opErr = err
		return false, err
	}

	existing, err := as.findAlarmRecordForMutation(ctx, roomID, channelID)
	if err != nil {
		opErr = err
		return false, opErr
	}
	if existing == nil {
		return false, nil
	}

	existingTypes, err := normalizeAlarmTypesStrict(existing.AlarmTypes, domain.DefaultAlarmTypes)
	if err != nil {
		opErr = err
		return false, err
	}

	effectiveRemovalTypes := intersectAlarmTypes(existingTypes, requestedRemovalTypes)
	if len(effectiveRemovalTypes) == 0 {
		return false, nil
	}

	remainingTypes := subtractAlarmTypes(existingTypes, effectiveRemovalTypes)
	removeRoomChannel := len(remainingTypes) == 0

	if removeRoomChannel {
		deleteErr := as.deleteAlarm(ctx, roomID, channelID)
		if deleteErr != nil {
			opErr = fmt.Errorf("delete alarm before cache removal: %w", deleteErr)
			if as.logger != nil {
				as.logger.Error("Failed to persist alarm removal before cache write", slog.Any("error", deleteErr))
			}
			return false, opErr
		}
	} else {
		updated := *existing
		updated.AlarmTypes = remainingTypes
		persistErr := as.updateAlarmTypes(ctx, &updated)
		if persistErr != nil {
			opErr = fmt.Errorf("persist alarm type update before cache removal: %w", persistErr)
			if as.logger != nil {
				as.logger.Error("Failed to persist alarm type update before cache write", slog.Any("error", persistErr))
			}
			return false, opErr
		}
	}

	removed, err := as.removeAlarmFromCache(ctx, roomID, channelID, effectiveRemovalTypes, removeRoomChannel)
	if err != nil {
		opErr = as.rebuildAlarmCacheFromRepository(ctx, "remove", fmt.Errorf("remove alarm: %w", err))
		if as.logger != nil {
			as.logger.Error("Failed to remove alarm", slog.Any("error", opErr))
		}
		return false, fmt.Errorf("rebuild remove cache from repository: %w", opErr)
	}

	if syncErr := as.syncPlatformMappingForChannel(ctx, channelID); syncErr != nil && as.logger != nil {
		as.logger.Warn("Failed to sync platform alarm mapping after remove",
			slog.Any("error", syncErr),
			slog.String("channel_id", channelID),
			slog.String("room_id", roomID),
		)
	}

	if as.logger != nil {
		as.logger.Info("Alarm removed",
			slog.String("room_id", roomID),
			slog.String("channel_id", channelID),
			slog.Any("alarm_types", effectiveRemovalTypes),
			slog.Any("remaining_alarm_types", remainingTypes),
		)
	}

	if as.alarmRepo != nil {
		return true, nil
	}

	return removed, nil
}

const cacheAlarmAtomicScript = `
local roomAlarmKey = ARGV[1]
local alarmRegistryKey = ARGV[2]
local channelRegistryKey = ARGV[3]
local memberNameKey = ARGV[4]
local roomNamesKey = ARGV[5]
local userNamesKey = ARGV[6]
local roomID = ARGV[7]
local channelID = ARGV[8]
local memberName = ARGV[9]
local roomName = ARGV[10]
local userID = ARGV[11]
local userName = ARGV[12]
local registryKey = ARGV[13]
local emptySubscriberCacheKey = ARGV[14]

local added = redis.call('SADD', roomAlarmKey, channelID)
redis.call('SADD', alarmRegistryKey, registryKey)
redis.call('SADD', channelRegistryKey, channelID)
redis.call('DEL', emptySubscriberCacheKey)

if memberName ~= '' then
  redis.call('HSET', memberNameKey, channelID, memberName)
end
if roomID ~= '' and roomName ~= '' then
  redis.call('HSET', roomNamesKey, roomID, roomName)
end
if userID ~= '' and userName ~= '' then
  redis.call('HSET', userNamesKey, userID, userName)
end

for i = 15, #ARGV do
  redis.call('SADD', ARGV[i], registryKey)
end

return added
`

func (as *AlarmService) cacheAlarm(ctx context.Context, record *domain.Alarm) (int64, error) {
	if record == nil {
		return 0, stdErrors.New("alarm is nil")
	}

	alarmTypes, err := normalizeAlarmTypesStrict(record.AlarmTypes, domain.DefaultAlarmTypes)
	if err != nil {
		return 0, err
	}
	record.AlarmTypes = alarmTypes

	added, err := as.cacheAlarmAtomic(ctx, record)
	if err == nil {
		return added, nil
	}

	return 0, err
}

func (as *AlarmService) cacheAlarmAtomic(ctx context.Context, record *domain.Alarm) (int64, error) {
	client, builder, ok := as.rawAlarmCacheEvalClient()
	if !ok {
		return as.cacheAlarmSequential(ctx, record)
	}

	registryKey := as.getRegistryKey(record.RoomID)
	args := []string{
		as.getAlarmKey(record.RoomID),
		AlarmRegistryKey,
		AlarmChannelRegistryKey,
		MemberNameKey,
		RoomNamesCacheKey,
		UserNamesCacheKey,
		record.RoomID,
		record.ChannelID,
		record.MemberName,
		record.RoomName,
		record.UserID,
		record.UserName,
		registryKey,
		sharedalarmkeys.AlarmSubscriberCacheEmptyKey,
	}
	for _, alarmType := range record.AlarmTypes {
		args = append(args, as.channelSubscribersKeyByType(record.ChannelID, alarmType))
	}

	resp := client.Do(ctx, builder.Eval().Script(cacheAlarmAtomicScript).Numkeys(0).Arg(args...).Build())
	added, err := resp.AsInt64()
	if err != nil {
		return 0, fmt.Errorf("atomic cache alarm: %w", err)
	}

	return added, nil
}

func (as *AlarmService) rawAlarmCacheEvalClient() (_ valkey.Client, _ valkey.Builder, ok bool) {
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()

	client := as.cache.GetClient()
	builder := as.cache.B()
	if client == nil {
		return nil, valkey.Builder{}, false
	}

	return client, builder, true
}

func (as *AlarmService) cacheAlarmSequential(ctx context.Context, record *domain.Alarm) (int64, error) {
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
	if err := as.cache.Del(ctx, sharedalarmkeys.AlarmSubscriberCacheEmptyKey); err != nil {
		return 0, fmt.Errorf("clear empty subscriber cache marker: %w", err)
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
	record, err := as.findAlarmRecordForMutation(ctx, roomID, channelID)
	if err != nil {
		return false, err
	}

	return record != nil, nil
}

func (as *AlarmService) findAlarmRecordForMutation(ctx context.Context, roomID, channelID string) (*domain.Alarm, error) {
	roomID = strings.TrimSpace(roomID)
	channelID = strings.TrimSpace(channelID)
	if roomID == "" || channelID == "" {
		return nil, nil
	}

	if as.alarmRepo != nil {
		alarms, err := findRoomAlarmsFromRepository(ctx, as.alarmRepo, roomID)
		if err != nil {
			return nil, fmt.Errorf("find room alarms: %w", err)
		}

		for _, alarm := range alarms {
			if alarm == nil || strings.TrimSpace(alarm.ChannelID) != channelID {
				continue
			}
			cloned := *alarm
			return &cloned, nil
		}

		return nil, nil
	}

	exists, err := as.cache.SIsMember(ctx, as.getAlarmKey(roomID), channelID)
	if err != nil {
		return nil, fmt.Errorf("check room alarm membership: %w", err)
	}
	if !exists {
		return nil, nil
	}

	registryKey := as.getRegistryKey(roomID)
	currentTypes := make(domain.AlarmTypes, 0, len(domain.AllAlarmTypes))
	for _, alarmType := range domain.AllAlarmTypes {
		subscriberKey := as.channelSubscribersKeyByType(channelID, alarmType)
		isSubscriber, err := as.cache.SIsMember(ctx, subscriberKey, registryKey)
		if err != nil {
			return nil, fmt.Errorf("check subscriber type %s: %w", alarmType, err)
		}
		if isSubscriber {
			currentTypes = append(currentTypes, alarmType)
		}
	}
	if len(currentTypes) == 0 {
		currentTypes = append(domain.AlarmTypes(nil), domain.DefaultAlarmTypes...)
	}

	return &domain.Alarm{
		RoomID:     roomID,
		ChannelID:  channelID,
		AlarmTypes: currentTypes,
	}, nil
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

func (as *AlarmService) removeAlarmFromCache(
	ctx context.Context,
	roomID string,
	channelID string,
	alarmTypes domain.AlarmTypes,
	removeRoomChannel bool,
) (bool, error) {
	alarmKey := as.getAlarmKey(roomID)
	removedRoomChannel := int64(0)

	if removeRoomChannel {
		removed, err := as.cache.SRem(ctx, alarmKey, []string{channelID})
		if err != nil {
			return false, fmt.Errorf("remove room alarm: %w", err)
		}
		removedRoomChannel = removed
	}

	registryKey := as.getRegistryKey(roomID)
	if err := as.removeChannelSubscribers(ctx, channelID, registryKey, alarmTypes); err != nil {
		return false, fmt.Errorf("remove channel subscribers: %w", err)
	}

	if err := as.cleanupChannelRegistryIfEmpty(ctx, channelID); err != nil {
		return false, fmt.Errorf("cleanup channel registry if empty: %w", err)
	}

	if removeRoomChannel {
		remainingAlarms, err := as.cache.SMembers(ctx, alarmKey)
		if err != nil {
			return false, fmt.Errorf("read remaining room alarms: %w", err)
		}

		if len(remainingAlarms) == 0 {
			if _, err := as.cache.SRem(ctx, AlarmRegistryKey, []string{registryKey}); err != nil {
				return false, fmt.Errorf("remove room registry: %w", err)
			}

			if as.logger != nil {
				as.logger.Info("Room removed from registry (no alarms left)", slog.String("room_id", roomID))
			}
		}
	}

	return removedRoomChannel > 0 || len(alarmTypes) > 0, nil
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
		if as.logger != nil {
			as.logger.Error("Failed to persist room alarm clear before cache write", slog.Any("error", deleteErr))
		}
		return 0, opErr
	}

	channelIDs := uniqueAlarmChannelIDs(alarmRecords)
	removed, err := as.clearRoomAlarmsFromCache(ctx, roomID, channelIDs)
	if err != nil {
		opErr = as.rebuildAlarmCacheFromRepository(ctx, "clear", fmt.Errorf("clear room alarms: %w", err))
		if as.logger != nil {
			as.logger.Error("Failed to clear room alarms", slog.Any("error", opErr))
		}
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

	if as.logger != nil {
		as.logger.Info("All alarms cleared",
			slog.String("room_id", roomID),
			slog.Int("count", len(channelIDs)),
		)
	}

	if as.alarmRepo != nil {
		return len(channelIDs), nil
	}

	return removed, nil
}
