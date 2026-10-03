package subscriptions

import (
	"context"
	"fmt"
	"log/slog"

	sharedlogging "github.com/park285/shared-go/v2/pkg/logging"

	"github.com/kapu/hololive-shared/pkg/privacylog"
)

// ClearRoomAlarms는 채팅방의 모든 채널·멤버별 구독을 해지하고 해지한 구독 수를 반환한다.
func (as *AlarmService) ClearRoomAlarms(ctx context.Context, roomID string) (int, error) {
	startedAt := as.lockCacheMutation("clear")
	defer as.cacheMutationMu.Unlock()

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

	if err := as.clearRoomAlarmsCacheMutation(ctx, roomID, channelIDs); err != nil {
		opErr = err
		return 0, fmt.Errorf("clear room alarms cache mutation: %w", err)
	}

	as.afterClearRoomAlarms(ctx, roomID, channelIDs)

	return len(alarmRecords), nil
}

func (as *AlarmService) deleteRoomAlarmsBeforeCacheClear(ctx context.Context, roomID string) error {
	if err := as.deleteRoomAlarms(ctx, roomID); err != nil {
		return sharedlogging.LogAndWrapError(ctx, as.logger, "delete room alarms before cache clear", err)
	}

	return nil
}

func (as *AlarmService) clearRoomAlarmsCacheMutation(ctx context.Context, roomID string, channelIDs []string) error {
	if err := as.clearRoomAlarmsFromCache(ctx, roomID, channelIDs); err != nil {
		opErr := as.rebuildAlarmCacheFromRepository(ctx, "clear", fmt.Errorf("clear room alarms: %w", err))

		return sharedlogging.LogAndWrapError(ctx, as.logger, "rebuild clear cache from repository", opErr)
	}

	if err := as.markAlarmCacheChanged(ctx); err != nil {
		opErr := as.rebuildAlarmCacheFromRepository(ctx, "clear_mark_changed", fmt.Errorf("mark alarm cache changed: %w", err))

		return sharedlogging.LogAndWrapError(ctx, as.logger, "mark room alarms changed in cache", opErr)
	}

	return nil
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
}
