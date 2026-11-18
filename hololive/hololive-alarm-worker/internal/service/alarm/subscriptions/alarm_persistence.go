package subscriptions

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/alarmstore"
	"github.com/kapu/hololive-shared/pkg/domain"
)

var (
	rebuildSubscriberCacheFromRepository = alarmstore.RebuildSubscriberCacheFromRepository
	findRoomAlarmsFromRepository         = func(ctx context.Context, repository *alarmstore.Repository, roomID string) ([]*domain.Alarm, error) {
		return repository.FindByRoom(ctx, roomID)
	}
)

func (as *AlarmService) persistAlarm(ctx context.Context, alarm *domain.Alarm) error {
	if alarm == nil {
		return nil
	}

	persistCtx, cancel := alarmPersistenceContext(ctx)
	defer cancel()

	if err := as.alarmWriter.Add(persistCtx, alarm); err != nil {
		return fmt.Errorf("persist alarm: %w", err)
	}

	return nil
}

func (as *AlarmService) deleteAlarm(ctx context.Context, roomID, channelID, hostID string) error {
	persistCtx, cancel := alarmPersistenceContext(ctx)
	defer cancel()

	if hostID != "" {
		if err := as.alarmWriter.RemoveHost(persistCtx, roomID, channelID, hostID); err != nil {
			return fmt.Errorf("delete member alarm: %w", err)
		}

		return nil
	}

	if err := as.alarmWriter.Remove(persistCtx, roomID, channelID); err != nil {
		return fmt.Errorf("delete alarm: %w", err)
	}

	return nil
}

func (as *AlarmService) deleteRoomAlarms(ctx context.Context, roomID string) error {
	persistCtx, cancel := alarmPersistenceContext(ctx)
	defer cancel()

	if _, err := as.alarmWriter.ClearByRoom(persistCtx, roomID); err != nil {
		return fmt.Errorf("delete room alarms: %w", err)
	}

	return nil
}

func alarmPersistenceContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, alarmPersistTaskTimeout)
}

func (as *AlarmService) rebuildAlarmCacheFromRepository(ctx context.Context, operation string, mutationErr error) error {
	if mutationErr == nil {
		return nil
	}

	startedAt := time.Now()

	var (
		rebuildErr error
		summary    alarmstore.CacheWarmSummary
	)

	defer func() {
		observeAlarmCacheRebuild(operation, rebuildErr)
		observeAlarmCacheRebuildDuration(operation, startedAt, rebuildErr)

		if rebuildErr == nil {
			observeAlarmCacheRebuildLoaded(operation, summary.AlarmCount, summary.RoomCount, summary.ChannelCount)
		}
	}()

	var err error

	summary, err = rebuildSubscriberCacheFromRepository(ctx, as.cache, as.alarmRepository)
	if err != nil {
		rebuildErr = err
		return errors.Join(mutationErr, fmt.Errorf("rebuild subscriber cache from DB: %w", err))
	}

	rebuildErr = nil

	return mutationErr
}

func (as *AlarmService) WarmCacheFromDB(ctx context.Context) error {
	startedAt := as.lockCacheMutation("warm")
	defer as.cacheMutationMu.Unlock()

	summary, err := rebuildSubscriberCacheFromRepository(ctx, as.cache, as.alarmRepository)
	observeAlarmCacheRebuild("warm", err)
	observeAlarmCacheRebuildDuration("warm", startedAt, err)

	if err != nil {
		return fmt.Errorf("rebuild subscriber cache from DB: %w", err)
	}

	observeAlarmCacheRebuildLoaded("warm", summary.AlarmCount, summary.RoomCount, summary.ChannelCount)

	if summary.AlarmCount == 0 {
		as.logger.Info("No alarms found in DB, cache warming skipped")

		return nil
	}

	as.logger.Info("Cache rebuilt from DB",
		slog.Int("alarms_loaded", summary.AlarmCount),
		slog.Int("rooms_loaded", summary.RoomCount),
		slog.Int("channels_loaded", summary.ChannelCount),
	)

	return nil
}
