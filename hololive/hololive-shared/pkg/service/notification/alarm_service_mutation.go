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
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
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
	if err := as.markAlarmCacheChanged(ctx); err != nil {
		opErr = as.rebuildAlarmCacheFromRepository(ctx, "remove_mark_changed", fmt.Errorf("mark alarm cache changed: %w", err))
		if as.logger != nil {
			as.logger.Error("Failed to mark alarm cache changed after remove", slog.Any("error", opErr))
		}
		return false, fmt.Errorf("rebuild remove cache version from repository: %w", opErr)
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
	if err := as.markAlarmCacheChanged(ctx); err != nil {
		opErr = as.rebuildAlarmCacheFromRepository(ctx, "clear_mark_changed", fmt.Errorf("mark alarm cache changed: %w", err))
		if as.logger != nil {
			as.logger.Error("Failed to mark alarm cache changed after clear", slog.Any("error", opErr))
		}
		return 0, fmt.Errorf("rebuild clear cache version from repository: %w", opErr)
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
