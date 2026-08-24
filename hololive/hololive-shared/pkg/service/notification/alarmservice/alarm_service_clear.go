package alarmservice

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	sharedlogging "github.com/park285/shared-go/v2/pkg/logging"

	"github.com/kapu/hololive-shared/pkg/privacylog"
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

	if deleteErr := as.deleteRoomAlarmsBeforeCacheClear(ctx, roomID); deleteErr != nil {
		opErr = deleteErr
		return 0, fmt.Errorf("delete room alarms before cache clear: %w", deleteErr)
	}

	channelIDs := uniqueAlarmChannelIDs(alarmRecords)

	removed, err := as.clearRoomAlarmsCacheMutation(ctx, roomID, channelIDs)
	if err != nil {
		opErr = err
		return 0, fmt.Errorf("clear room alarms cache mutation: %w", err)
	}

	as.afterClearRoomAlarms(ctx, roomID, channelIDs)

	if as.alarmRepository != nil {
		return len(channelIDs), nil
	}

	return removed, nil
}

func (as *AlarmService) deleteRoomAlarmsBeforeCacheClear(ctx context.Context, roomID string) error {
	if err := as.deleteRoomAlarms(ctx, roomID); err != nil {
		if logErr := sharedlogging.LogAndWrapError(ctx, as.logger, "delete room alarms before cache clear", err); logErr != nil {
			return fmt.Errorf("log and wrap error: %w", logErr)
		}

		return nil
	}

	return nil
}

func (as *AlarmService) clearRoomAlarmsCacheMutation(ctx context.Context, roomID string, channelIDs []string) (int, error) {
	removed, err := as.clearRoomAlarmsFromCache(ctx, roomID, channelIDs)
	if err != nil {
		opErr := as.rebuildAlarmCacheFromRepository(ctx, "clear", fmt.Errorf("clear room alarms: %w", err))
		if err := sharedlogging.LogAndWrapError(ctx, as.logger, "rebuild clear cache from repository", opErr); err != nil {
			return 0, fmt.Errorf("log and wrap error: %w", err)
		}

		return 0, nil
	}

	if err := as.markAlarmCacheChanged(ctx); err != nil {
		opErr := as.rebuildAlarmCacheFromRepository(ctx, "clear_mark_changed", fmt.Errorf("mark alarm cache changed: %w", err))
		if err := sharedlogging.LogAndWrapError(ctx, as.logger, "mark room alarms changed in cache", opErr); err != nil {
			return 0, fmt.Errorf("log and wrap error: %w", err)
		}

		return 0, nil
	}

	return removed, nil
}

func (as *AlarmService) afterClearRoomAlarms(ctx context.Context, roomID string, channelIDs []string) {
	for _, channelID := range channelIDs {
		as.cleanupClearedRoomAlarmChannel(ctx, roomID, channelID)
	}

	if as.logger != nil {
		as.logger.Info("All alarms cleared",
			privacylog.RoomIDAttr(roomID),
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
			privacylog.RoomIDAttr(roomID),
			slog.String("channel_id", channelID),
		)
	}

	if syncErr := as.syncPlatformMappingForChannel(ctx, channelID); syncErr != nil && as.logger != nil {
		sharedlogging.LogWarnWithErrorAttrs(ctx, as.logger,
			"sync platform alarm mapping after clear.failed",
			"Failed to sync platform alarm mapping after clear",
			syncErr,
			privacylog.RoomIDAttr(roomID),
			slog.String("channel_id", channelID),
		)
	}
}
