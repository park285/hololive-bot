package alarmservice

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	sharedlogging "github.com/park285/shared-go/pkg/logging"

	"github.com/kapu/hololive-shared/pkg/domain"
)

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

func normalizeRemoveAlarmRequest(roomID, channelID string, alarmTypes domain.AlarmTypes) (normalizedRoomID, normalizedChannelID string, removalTypes domain.AlarmTypes, err error) {
	roomID = strings.TrimSpace(roomID)
	channelID = strings.TrimSpace(channelID)
	if roomID == "" || channelID == "" {
		return "", "", nil, fmt.Errorf("room_id and channel_id are required")
	}
	requestedRemovalTypes, err := normalizeAlarmTypesStrict(alarmTypes, domain.AllAlarmTypes)
	return roomID, channelID, requestedRemovalTypes, err
}

func (as *AlarmService) prepareRemoveAlarmMutation(ctx context.Context, roomID, channelID string, requestedRemovalTypes domain.AlarmTypes) (removeAlarmMutation, bool, error) {
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

func (as *AlarmService) persistRemoveAlarmMutation(ctx context.Context, roomID, channelID string, mutation removeAlarmMutation) error {
	if mutation.removeRoomChannel {
		return as.deleteAlarmBeforeCacheRemoval(ctx, roomID, channelID)
	}
	return as.updateAlarmTypesBeforeCacheRemoval(ctx, mutation.updatedRecord)
}

func (as *AlarmService) deleteAlarmBeforeCacheRemoval(ctx context.Context, roomID, channelID string) error {
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

func (as *AlarmService) removeAlarmCacheMutation(ctx context.Context, roomID, channelID string, mutation removeAlarmMutation) (bool, error) {
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

func (as *AlarmService) afterRemoveAlarm(ctx context.Context, roomID, channelID string, mutation removeAlarmMutation) {
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
