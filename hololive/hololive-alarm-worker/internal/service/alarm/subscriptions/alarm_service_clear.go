package subscriptions

import (
	"context"
	"fmt"
	"log/slog"

	sharedlogging "github.com/park285/shared-go/v2/pkg/logging"

	"github.com/kapu/hololive-shared/pkg/cleanupctx"
	"github.com/kapu/hololive-shared/pkg/domain"
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

	channelIDs := uniqueAlarmChannelIDs(alarmRecords)
	for _, channelID := range channelIDs {
		if err := as.invalidateChannelSubscribers(ctx, channelID, domain.AllAlarmTypes); err != nil {
			opErr = err
			return 0, fmt.Errorf("invalidate subscribers before room clear: %w", err)
		}
	}

	if deleteErr := as.deleteRoomAlarmsBeforeCacheClear(ctx, roomID); deleteErr != nil {
		opErr = deleteErr
		return 0, fmt.Errorf("delete room alarms before cache clear: %w", deleteErr)
	}

	cacheCtx, cancel := cleanupctx.WithTimeout(ctx, cleanupctx.DefaultTimeout)
	defer cancel()

	if err := as.clearRoomAlarmsCacheMutation(cacheCtx, channelIDs); err != nil {
		opErr = err
		return 0, fmt.Errorf("clear room alarms cache mutation: %w", err)
	}

	as.afterClearRoomAlarms(roomID, channelIDs)

	return len(alarmRecords), nil
}

func (as *AlarmService) deleteRoomAlarmsBeforeCacheClear(ctx context.Context, roomID string) error {
	if err := as.deleteRoomAlarms(ctx, roomID); err != nil {
		return sharedlogging.LogAndWrapError(ctx, as.logger, "delete room alarms before cache clear", err)
	}

	return nil
}

func (as *AlarmService) clearRoomAlarmsCacheMutation(ctx context.Context, channelIDs []string) error {
	for _, channelID := range channelIDs {
		if err := as.cleanupChannelRegistryIfEmpty(ctx, channelID); err != nil {
			opErr := as.rebuildAlarmCacheFromRepository(ctx, "clear", fmt.Errorf("clear room alarms: %w", err))

			return sharedlogging.LogAndWrapError(ctx, as.logger, "rebuild clear cache from repository", opErr)
		}
	}

	if err := as.markAlarmCacheChanged(ctx); err != nil {
		opErr := as.rebuildAlarmCacheFromRepository(ctx, "clear_mark_changed", fmt.Errorf("mark alarm cache changed: %w", err))

		return sharedlogging.LogAndWrapError(ctx, as.logger, "mark room alarms changed in cache", opErr)
	}

	return nil
}

func (as *AlarmService) afterClearRoomAlarms(roomID string, channelIDs []string) {
	if as.logger != nil {
		as.logger.Info("All alarms cleared",
			privacylog.RoomIDAttr(roomID),
			slog.Int("count", len(channelIDs)),
		)
	}
}
