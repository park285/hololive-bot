package alarmservice

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	sharedlogging "github.com/park285/shared-go/pkg/logging"
)

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

func (as *AlarmService) cleanupClearedRoomAlarmChannel(ctx context.Context, roomID, channelID string) {
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
