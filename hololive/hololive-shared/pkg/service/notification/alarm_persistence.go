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
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	sharedalarm "github.com/kapu/hololive-shared/pkg/service/alarm"
)

var (
	rebuildSubscriberCacheFromRepository = sharedalarm.RebuildSubscriberCacheFromRepository
	findRoomAlarmsFromRepository         = func(ctx context.Context, repo *sharedalarm.Repository, roomID string) ([]*domain.Alarm, error) {
		return repo.FindByRoom(ctx, roomID)
	}
)

type alarmUpsertWriter interface {
	Upsert(ctx context.Context, alarm *domain.Alarm) error
}

type alarmTypesUpdater interface {
	UpdateTypes(ctx context.Context, roomID string, channelID string, alarmTypes domain.AlarmTypes) error
}

func (as *AlarmService) persistAlarm(ctx context.Context, alarm *domain.Alarm) error {
	if as.alarmWriter == nil || alarm == nil {
		return nil
	}

	if ctx == nil {
		ctx = context.Background()
	}

	persistCtx, cancel := context.WithTimeout(ctx, alarmPersistTaskTimeout)
	defer cancel()

	if writer, ok := as.alarmWriter.(alarmUpsertWriter); ok {
		if err := writer.Upsert(persistCtx, alarm); err != nil {
			return fmt.Errorf("upsert alarm: %w", err)
		}

		return nil
	}

	if err := as.alarmWriter.Add(persistCtx, alarm); err != nil {
		return fmt.Errorf("persist alarm: %w", err)
	}

	return nil
}

func (as *AlarmService) updateAlarmTypes(ctx context.Context, alarm *domain.Alarm) error {
	if as.alarmWriter == nil || alarm == nil {
		return nil
	}

	if ctx == nil {
		ctx = context.Background()
	}

	persistCtx, cancel := context.WithTimeout(ctx, alarmPersistTaskTimeout)
	defer cancel()

	if writer, ok := as.alarmWriter.(alarmTypesUpdater); ok {
		return updateAlarmTypesWithWriter(persistCtx, writer, alarm)
	}

	if writer, ok := as.alarmWriter.(alarmUpsertWriter); ok {
		return upsertAlarmTypeUpdate(persistCtx, writer, alarm)
	}

	if err := as.alarmWriter.Add(persistCtx, alarm); err != nil {
		return fmt.Errorf("persist alarm type update: %w", err)
	}

	return nil
}

func updateAlarmTypesWithWriter(ctx context.Context, writer alarmTypesUpdater, alarm *domain.Alarm) error {
	if err := writer.UpdateTypes(ctx, alarm.RoomID, alarm.ChannelID, alarm.AlarmTypes); err != nil {
		return fmt.Errorf("update alarm types: %w", err)
	}

	return nil
}

func upsertAlarmTypeUpdate(ctx context.Context, writer alarmUpsertWriter, alarm *domain.Alarm) error {
	if err := writer.Upsert(ctx, alarm); err != nil {
		return fmt.Errorf("upsert alarm type update: %w", err)
	}

	return nil
}

func (as *AlarmService) deleteAlarm(ctx context.Context, roomID, channelID string) error {
	if as.alarmWriter == nil {
		return nil
	}

	if ctx == nil {
		ctx = context.Background()
	}

	persistCtx, cancel := context.WithTimeout(ctx, alarmPersistTaskTimeout)
	defer cancel()

	if err := as.alarmWriter.Remove(persistCtx, roomID, channelID); err != nil {
		return fmt.Errorf("delete alarm: %w", err)
	}

	return nil
}

func (as *AlarmService) deleteRoomAlarms(ctx context.Context, roomID string) error {
	if as.alarmWriter == nil {
		return nil
	}

	if ctx == nil {
		ctx = context.Background()
	}

	persistCtx, cancel := context.WithTimeout(ctx, alarmPersistTaskTimeout)
	defer cancel()

	if _, err := as.alarmWriter.ClearByRoom(persistCtx, roomID); err != nil {
		return fmt.Errorf("delete room alarms: %w", err)
	}

	return nil
}

func (as *AlarmService) rebuildAlarmCacheFromRepository(ctx context.Context, operation string, mutationErr error) error {
	if mutationErr == nil {
		return nil
	}

	startedAt := time.Now()

	var (
		rebuildErr error
		summary    sharedalarm.CacheWarmSummary
	)

	defer func() {
		observeAlarmCacheRebuild(operation, rebuildErr)
		observeAlarmCacheRebuildDuration(operation, startedAt, rebuildErr)

		if rebuildErr == nil {
			observeAlarmCacheRebuildLoaded(operation, summary.AlarmCount, summary.RoomCount, summary.ChannelCount)
		}
	}()

	if as.alarmRepo == nil {
		rebuildErr = mutationErr
		return mutationErr
	}

	var err error

	summary, err = rebuildSubscriberCacheFromRepository(ctx, as.cache, as.alarmRepo)
	if err != nil {
		rebuildErr = err
		return errors.Join(mutationErr, fmt.Errorf("rebuild subscriber cache from DB: %w", err))
	}

	rebuildErr = nil

	return mutationErr
}

// 이 메서드는 앱 시작 시 한 번만 호출되며, 이후 런타임 중에는 Valkey만 사용한다.
func (as *AlarmService) WarmCacheFromDB(ctx context.Context) error {
	if as.alarmRepo == nil {
		as.logger.Info("Alarm repository not configured, skipping cache warming")
		return nil
	}

	startedAt := time.Now()

	summary, err := rebuildSubscriberCacheFromRepository(ctx, as.cache, as.alarmRepo)
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
