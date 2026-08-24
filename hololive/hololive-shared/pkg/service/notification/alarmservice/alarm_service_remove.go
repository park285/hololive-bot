package alarmservice

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	sharedlogging "github.com/park285/shared-go/v2/pkg/logging"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/privacylog"
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
		return false, fmt.Errorf("normalize remove alarm request: %w", err)
	}

	mutation, found, err := as.prepareRemoveAlarmMutation(ctx, roomID, channelID, requestedRemovalTypes)
	if err != nil {
		opErr = err
		return false, fmt.Errorf("prepare remove alarm mutation: %w", err)
	}

	if !found {
		return false, nil
	}

	if persistErr := as.persistRemoveAlarmMutation(ctx, roomID, channelID, mutation); persistErr != nil {
		opErr = persistErr
		return false, fmt.Errorf("persist remove alarm mutation: %w", persistErr)
	}

	removed, err := as.removeAlarmCacheMutation(ctx, roomID, channelID, mutation)
	if err != nil {
		opErr = err
		return false, fmt.Errorf("remove alarm cache mutation: %w", err)
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
		return "", "", nil, errors.New("room_id and channel_id are required")
	}

	requestedRemovalTypes, err := normalizeAlarmTypesStrict(alarmTypes, domain.AllAlarmTypes)
	if err != nil {
		return "", "", nil, fmt.Errorf("normalize alarm types strict: %w", err)
	}

	return roomID, channelID, requestedRemovalTypes, nil
}

func (as *AlarmService) prepareRemoveAlarmMutation(ctx context.Context, roomID, channelID string, requestedRemovalTypes domain.AlarmTypes) (removeAlarmMutation, bool, error) {
	existing, err := as.findAlarmRecordForMutation(ctx, roomID, channelID)
	if errors.Is(err, errAlarmRecordNotFound) {
		return removeAlarmMutation{}, false, nil
	}

	if err != nil {
		return removeAlarmMutation{}, false, fmt.Errorf("find alarm record for mutation: %w", err)
	}

	existingTypes, err := normalizeAlarmTypesStrict(existing.AlarmTypes, domain.DefaultAlarmTypes)
	if err != nil {
		return removeAlarmMutation{}, false, fmt.Errorf("normalize alarm types strict: %w", err)
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
		if err := as.deleteAlarmBeforeCacheRemoval(ctx, roomID, channelID); err != nil {
			return fmt.Errorf("delete alarm before cache removal: %w", err)
		}

		return nil
	}

	if err := as.updateAlarmTypesBeforeCacheRemoval(ctx, mutation.updatedRecord); err != nil {
		return fmt.Errorf("update alarm types before cache removal: %w", err)
	}

	return nil
}

func (as *AlarmService) deleteAlarmBeforeCacheRemoval(ctx context.Context, roomID, channelID string) error {
	if err := as.deleteAlarm(ctx, roomID, channelID); err != nil {
		if logErr := sharedlogging.LogAndWrapError(ctx, as.logger, "delete alarm before cache removal", err); logErr != nil {
			return fmt.Errorf("log and wrap error: %w", logErr)
		}

		return nil
	}

	return nil
}

func (as *AlarmService) updateAlarmTypesBeforeCacheRemoval(ctx context.Context, updated *domain.Alarm) error {
	if err := as.updateAlarmTypes(ctx, updated); err != nil {
		if logAndErr := sharedlogging.LogAndWrapError(ctx, as.logger, "persist alarm type update before cache removal", err); logAndErr != nil {
			return fmt.Errorf("log and wrap error: %w", logAndErr)
		}

		return nil
	}

	return nil
}

func (as *AlarmService) removeAlarmCacheMutation(ctx context.Context, roomID, channelID string, mutation removeAlarmMutation) (bool, error) {
	removed, err := as.removeAlarmFromCache(ctx, roomID, channelID, mutation.effectiveRemovalTypes, mutation.removeRoomChannel)
	if err != nil {
		opErr := as.rebuildAlarmCacheFromRepository(ctx, "remove", fmt.Errorf("remove alarm: %w", err))
		if err := sharedlogging.LogAndWrapError(ctx, as.logger, "rebuild remove cache from repository", opErr); err != nil {
			return false, fmt.Errorf("log and wrap error: %w", err)
		}

		return false, nil
	}

	if err := as.markAlarmCacheChanged(ctx); err != nil {
		opErr := as.rebuildAlarmCacheFromRepository(ctx, "remove_mark_changed", fmt.Errorf("mark alarm cache changed: %w", err))
		if err := sharedlogging.LogAndWrapError(ctx, as.logger, "mark room alarms changed in cache", opErr); err != nil {
			return false, fmt.Errorf("log and wrap error: %w", err)
		}

		return false, nil
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
			privacylog.RoomIDAttr(roomID),
		)
	}

	if as.logger != nil {
		as.logger.Info("Alarm removed",
			privacylog.RoomIDAttr(roomID),
			slog.String("channel_id", channelID),
			slog.Any("alarm_types", mutation.effectiveRemovalTypes),
			slog.Any("remaining_alarm_types", mutation.remainingTypes),
		)
	}
}
