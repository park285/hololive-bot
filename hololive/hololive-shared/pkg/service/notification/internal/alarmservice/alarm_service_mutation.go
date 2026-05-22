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

package alarmservice

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	sharedlogging "github.com/park285/llm-kakao-bots/shared-go/pkg/logging"

	"github.com/kapu/hololive-shared/pkg/domain"
)

type addAlarmMutation struct {
	record          *domain.Alarm
	cacheRecord     domain.Alarm
	newlyAddedTypes domain.AlarmTypes
	existing        bool
}

type removeAlarmMutation struct {
	effectiveRemovalTypes domain.AlarmTypes
	remainingTypes        domain.AlarmTypes
	removeRoomChannel     bool
	updatedRecord         *domain.Alarm
}

func (as *AlarmService) AddAlarm(ctx context.Context, req domain.AddAlarmRequest) (bool, error) {
	startedAt := time.Now()

	var opErr error

	defer func() {
		observeAlarmServiceOperation("add", startedAt, opErr)
	}()

	req, err := normalizeAddAlarmRequest(req)
	if err != nil {
		opErr = err
		return false, err
	}

	requestedTypes, err := normalizeAlarmTypesStrict(req.AlarmTypes, domain.DefaultAlarmTypes)
	if err != nil {
		opErr = err
		return false, err
	}

	mutation, shouldAdd, err := as.prepareAddAlarmMutation(ctx, req, requestedTypes)
	if err != nil {
		opErr = err
		return false, err
	}
	if !shouldAdd {
		return false, nil
	}

	if err := as.persistAddAlarmMutation(ctx, mutation); err != nil {
		opErr = err
		return false, err
	}
	added, err := as.cacheAddAlarmMutation(ctx, mutation)
	if err != nil {
		opErr = err
		return false, err
	}

	as.afterAddAlarm(ctx, req, mutation.newlyAddedTypes)

	return added > 0 || mutation.existing, nil
}

func (as *AlarmService) cacheAddAlarmMutation(ctx context.Context, mutation addAlarmMutation) (int64, error) {
	added, err := as.cacheAlarm(ctx, &mutation.cacheRecord)
	if err == nil {
		return added, nil
	}
	opErr := as.rebuildAlarmCacheFromRepository(ctx, "add", fmt.Errorf("add alarm: %w", err))
	return 0, sharedlogging.LogAndWrapError(ctx, as.logger, "rebuild add cache from repository", opErr)
}

func normalizeAddAlarmRequest(req domain.AddAlarmRequest) (domain.AddAlarmRequest, error) {
	req.RoomID = strings.TrimSpace(req.RoomID)
	req.UserID = strings.TrimSpace(req.UserID)
	req.ChannelID = strings.TrimSpace(req.ChannelID)
	req.MemberName = strings.TrimSpace(req.MemberName)
	req.RoomName = strings.TrimSpace(req.RoomName)
	req.UserName = strings.TrimSpace(req.UserName)
	if req.RoomID == "" || req.ChannelID == "" {
		return req, fmt.Errorf("room_id and channel_id are required")
	}
	return req, nil
}

func (as *AlarmService) prepareAddAlarmMutation(ctx context.Context, req domain.AddAlarmRequest, requestedTypes domain.AlarmTypes) (addAlarmMutation, bool, error) {
	existing, err := as.findAlarmRecordForMutation(ctx, req.RoomID, req.ChannelID)
	if err != nil {
		return addAlarmMutation{}, false, err
	}

	mergedTypes, newlyAddedTypes, err := addAlarmTypeMutation(existing, requestedTypes)
	if err != nil {
		return addAlarmMutation{}, false, err
	}
	if len(newlyAddedTypes) == 0 {
		return addAlarmMutation{}, false, nil
	}

	record := buildAlarmRecord(req, mergedTypes)
	cacheRecord := *record
	if existing != nil {
		cacheRecord.AlarmTypes = newlyAddedTypes
	}
	return addAlarmMutation{record: record, cacheRecord: cacheRecord, newlyAddedTypes: newlyAddedTypes, existing: existing != nil}, true, nil
}

func addAlarmTypeMutation(existing *domain.Alarm, requestedTypes domain.AlarmTypes) (domain.AlarmTypes, domain.AlarmTypes, error) {
	if existing == nil {
		return requestedTypes, requestedTypes, nil
	}
	existingTypes, err := normalizeAlarmTypesStrict(existing.AlarmTypes, domain.DefaultAlarmTypes)
	if err != nil {
		return nil, nil, err
	}
	mergedTypes := mergeAlarmTypes(existingTypes, requestedTypes)
	return mergedTypes, subtractAlarmTypes(mergedTypes, existingTypes), nil
}

func (as *AlarmService) persistAddAlarmMutation(ctx context.Context, mutation addAlarmMutation) error {
	var err error
	if mutation.existing {
		err = as.updateAlarmTypes(ctx, mutation.record)
	} else {
		err = as.persistAlarm(ctx, mutation.record)
	}
	if err != nil {
		return sharedlogging.LogAndWrapError(ctx, as.logger, "persist alarm before cache write", err)
	}
	return nil
}

func (as *AlarmService) afterAddAlarm(ctx context.Context, req domain.AddAlarmRequest, newlyAddedTypes domain.AlarmTypes) {
	as.logAlarmAdded(req, newlyAddedTypes)
	if syncErr := as.syncPlatformMappingForChannel(ctx, req.ChannelID); syncErr != nil && as.logger != nil {
		sharedlogging.LogWarnWithErrorAttrs(ctx, as.logger,
			"sync platform alarm mapping after add.failed",
			"Failed to sync platform alarm mapping after add",
			syncErr,
			slog.String("channel_id", req.ChannelID),
			slog.String("room_id", req.RoomID),
		)
	}
}

func (as *AlarmService) RemoveAlarm(ctx context.Context, roomID, channelID string, alarmTypes domain.AlarmTypes) (bool, error) {
	startedAt := time.Now()

	var opErr error

	defer func() {
		observeAlarmServiceOperation("remove", startedAt, opErr)
	}()

	roomID, channelID, requestedRemovalTypes, err := normalizeRemoveAlarmRequest(roomID, channelID, alarmTypes)
	if err != nil {
		opErr = err
		return false, err
	}

	mutation, found, err := as.prepareRemoveAlarmMutation(ctx, roomID, channelID, requestedRemovalTypes)
	if err != nil {
		opErr = err
		return false, err
	}
	if !found {
		return false, nil
	}

	if err := as.persistRemoveAlarmMutation(ctx, roomID, channelID, mutation); err != nil {
		opErr = err
		return false, err
	}

	removed, err := as.removeAlarmCacheMutation(ctx, roomID, channelID, mutation)
	if err != nil {
		opErr = err
		return false, err
	}
	as.afterRemoveAlarm(ctx, roomID, channelID, mutation)

	if as.alarmRepository != nil {
		return true, nil
	}

	return removed, nil
}

func normalizeRemoveAlarmRequest(roomID string, channelID string, alarmTypes domain.AlarmTypes) (string, string, domain.AlarmTypes, error) {
	roomID = strings.TrimSpace(roomID)
	channelID = strings.TrimSpace(channelID)
	if roomID == "" || channelID == "" {
		return "", "", nil, fmt.Errorf("room_id and channel_id are required")
	}
	requestedRemovalTypes, err := normalizeAlarmTypesStrict(alarmTypes, domain.AllAlarmTypes)
	return roomID, channelID, requestedRemovalTypes, err
}

func (as *AlarmService) prepareRemoveAlarmMutation(ctx context.Context, roomID string, channelID string, requestedRemovalTypes domain.AlarmTypes) (removeAlarmMutation, bool, error) {
	existing, err := as.findAlarmRecordForMutation(ctx, roomID, channelID)
	if err != nil {
		return removeAlarmMutation{}, false, err
	}
	if existing == nil {
		return removeAlarmMutation{}, false, nil
	}

	existingTypes, err := normalizeAlarmTypesStrict(existing.AlarmTypes, domain.DefaultAlarmTypes)
	if err != nil {
		return removeAlarmMutation{}, false, err
	}
	effectiveRemovalTypes := intersectAlarmTypes(existingTypes, requestedRemovalTypes)
	if len(effectiveRemovalTypes) == 0 {
		return removeAlarmMutation{}, false, nil
	}

	remainingTypes := subtractAlarmTypes(existingTypes, effectiveRemovalTypes)
	updated := *existing
	updated.AlarmTypes = remainingTypes
	return removeAlarmMutation{
		effectiveRemovalTypes: effectiveRemovalTypes,
		remainingTypes:        remainingTypes,
		removeRoomChannel:     len(remainingTypes) == 0,
		updatedRecord:         &updated,
	}, true, nil
}

func (as *AlarmService) persistRemoveAlarmMutation(ctx context.Context, roomID string, channelID string, mutation removeAlarmMutation) error {
	if mutation.removeRoomChannel {
		return as.deleteAlarmBeforeCacheRemoval(ctx, roomID, channelID)
	}
	return as.updateAlarmTypesBeforeCacheRemoval(ctx, mutation.updatedRecord)
}

func (as *AlarmService) deleteAlarmBeforeCacheRemoval(ctx context.Context, roomID string, channelID string) error {
	if err := as.deleteAlarm(ctx, roomID, channelID); err != nil {
		return sharedlogging.LogAndWrapError(ctx, as.logger, "delete alarm before cache removal", err)
	}
	return nil
}

func (as *AlarmService) updateAlarmTypesBeforeCacheRemoval(ctx context.Context, updated *domain.Alarm) error {
	if err := as.updateAlarmTypes(ctx, updated); err != nil {
		return sharedlogging.LogAndWrapError(ctx, as.logger, "persist alarm type update before cache removal", err)
	}
	return nil
}

func (as *AlarmService) removeAlarmCacheMutation(ctx context.Context, roomID string, channelID string, mutation removeAlarmMutation) (bool, error) {
	removed, err := as.removeAlarmFromCache(ctx, roomID, channelID, mutation.effectiveRemovalTypes, mutation.removeRoomChannel)
	if err != nil {
		opErr := as.rebuildAlarmCacheFromRepository(ctx, "remove", fmt.Errorf("remove alarm: %w", err))
		return false, sharedlogging.LogAndWrapError(ctx, as.logger, "rebuild remove cache from repository", opErr)
	}
	if err := as.markAlarmCacheChanged(ctx); err != nil {
		opErr := as.rebuildAlarmCacheFromRepository(ctx, "remove_mark_changed", fmt.Errorf("mark alarm cache changed: %w", err))
		return false, sharedlogging.LogAndWrapError(ctx, as.logger, "mark room alarms changed in cache", opErr)
	}
	return removed, nil
}

func (as *AlarmService) afterRemoveAlarm(ctx context.Context, roomID string, channelID string, mutation removeAlarmMutation) {
	if syncErr := as.syncPlatformMappingForChannel(ctx, channelID); syncErr != nil && as.logger != nil {
		sharedlogging.LogWarnWithErrorAttrs(ctx, as.logger,
			"sync platform alarm mapping after remove.failed",
			"Failed to sync platform alarm mapping after remove",
			syncErr,
			slog.String("channel_id", channelID),
			slog.String("room_id", roomID),
		)
	}
	if as.logger != nil {
		as.logger.Info("Alarm removed",
			slog.String("room_id", roomID),
			slog.String("channel_id", channelID),
			slog.Any("alarm_types", mutation.effectiveRemovalTypes),
			slog.Any("remaining_alarm_types", mutation.remainingTypes),
		)
	}
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

	if err := as.deleteRoomAlarmsBeforeCacheClear(ctx, roomID); err != nil {
		opErr = err
		return 0, err
	}

	channelIDs := uniqueAlarmChannelIDs(alarmRecords)
	removed, err := as.clearRoomAlarmsCacheMutation(ctx, roomID, channelIDs)
	if err != nil {
		opErr = err
		return 0, err
	}

	as.afterClearRoomAlarms(ctx, roomID, channelIDs)

	if as.alarmRepository != nil {
		return len(channelIDs), nil
	}

	return removed, nil
}

func (as *AlarmService) deleteRoomAlarmsBeforeCacheClear(ctx context.Context, roomID string) error {
	if err := as.deleteRoomAlarms(ctx, roomID); err != nil {
		return sharedlogging.LogAndWrapError(ctx, as.logger, "delete room alarms before cache clear", err)
	}
	return nil
}

func (as *AlarmService) clearRoomAlarmsCacheMutation(ctx context.Context, roomID string, channelIDs []string) (int, error) {
	removed, err := as.clearRoomAlarmsFromCache(ctx, roomID, channelIDs)
	if err != nil {
		opErr := as.rebuildAlarmCacheFromRepository(ctx, "clear", fmt.Errorf("clear room alarms: %w", err))
		return 0, sharedlogging.LogAndWrapError(ctx, as.logger, "rebuild clear cache from repository", opErr)
	}
	if err := as.markAlarmCacheChanged(ctx); err != nil {
		opErr := as.rebuildAlarmCacheFromRepository(ctx, "clear_mark_changed", fmt.Errorf("mark alarm cache changed: %w", err))
		return 0, sharedlogging.LogAndWrapError(ctx, as.logger, "mark room alarms changed in cache", opErr)
	}
	return removed, nil
}

func (as *AlarmService) afterClearRoomAlarms(ctx context.Context, roomID string, channelIDs []string) {
	for _, channelID := range channelIDs {
		as.cleanupClearedRoomAlarmChannel(ctx, roomID, channelID)
	}
	if as.logger != nil {
		as.logger.Info("All alarms cleared",
			slog.String("room_id", roomID),
			slog.Int("count", len(channelIDs)),
		)
	}
}

func (as *AlarmService) cleanupClearedRoomAlarmChannel(ctx context.Context, roomID string, channelID string) {
	if err := as.cleanupChannelRegistryIfEmpty(ctx, channelID); err != nil && as.logger != nil {
		sharedlogging.LogWarnWithErrorAttrs(ctx, as.logger,
			"cleanup channel registry during room alarm clear.failed",
			"Failed to cleanup channel registry during room alarm clear",
			err,
			slog.String("room_id", roomID),
			slog.String("channel_id", channelID),
		)
	}

	if syncErr := as.syncPlatformMappingForChannel(ctx, channelID); syncErr != nil && as.logger != nil {
		sharedlogging.LogWarnWithErrorAttrs(ctx, as.logger,
			"sync platform alarm mapping after clear.failed",
			"Failed to sync platform alarm mapping after clear",
			syncErr,
			slog.String("room_id", roomID),
			slog.String("channel_id", channelID),
		)
	}
}
