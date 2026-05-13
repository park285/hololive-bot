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

package scheduler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	sharedalarmkeys "github.com/kapu/hololive-shared/pkg/service/alarm/keys"
	"github.com/kapu/hololive-shared/pkg/service/cache"
)

const (
	alarmCacheRecoveryInterval = time.Minute
	alarmCacheRecoveryTimeout  = 10 * time.Second
)

type alarmCacheWarmer interface {
	WarmCacheFromDB(ctx context.Context) error
}

type alarmPlatformMappingSyncer interface {
	SyncPlatformMappings(ctx context.Context) error
}

func (s *RuntimeScheduler) runAlarmCacheRecoveryLoop(ctx context.Context) error {
	if s == nil {
		return nil
	}

	ticker := time.NewTicker(alarmCacheRecoveryInterval)
	defer ticker.Stop()

	for {
		if !waitAlarmCacheRecoveryTick(ctx, ticker.C) {
			s.logger.Info("Alarm cache recovery loop stopped")
			return ctx.Err()
		}
		s.runAlarmCacheRecoveryCheck(ctx)
	}
}

func waitAlarmCacheRecoveryTick(ctx context.Context, ticks <-chan time.Time) bool {
	select {
	case <-ctx.Done():
		return false
	case <-ticks:
		return true
	}
}

func (s *RuntimeScheduler) runAlarmCacheRecoveryCheck(ctx context.Context) {
	recoveryCtx, cancel := context.WithTimeout(ctx, alarmCacheRecoveryTimeout)
	err := s.recoverAlarmCacheIfRegistryEmpty(recoveryCtx, "periodic")
	cancel()
	if err != nil {
		s.logger.Warn("Alarm cache recovery check failed", slog.Any("error", err))
	}
}

func (s *RuntimeScheduler) recoverAlarmCacheIfRegistryEmpty(ctx context.Context, reason string) error {
	if s == nil || s.cacheSvc == nil || s.alarmCacheWarmer == nil {
		return nil
	}

	registryExists, err := s.cacheSvc.Exists(ctx, sharedalarmkeys.AlarmChannelRegistryKey)
	if err != nil {
		return fmt.Errorf("recover alarm cache: check channel registry: %w", err)
	}
	if registryExists {
		return s.syncPlatformMappingsIfMissing(ctx)
	}

	emptyCache, err := s.cacheSvc.Exists(ctx, sharedalarmkeys.AlarmSubscriberCacheEmptyKey)
	if err != nil {
		return fmt.Errorf("recover alarm cache: check empty marker: %w", err)
	}
	if emptyCache {
		return nil
	}

	return s.recoverAlarmCacheFromDB(ctx, reason)
}

func (s *RuntimeScheduler) recoverAlarmCacheFromDB(ctx context.Context, reason string) error {
	if err := s.alarmCacheWarmer.WarmCacheFromDB(ctx); err != nil {
		return fmt.Errorf("recover alarm cache: warm cache from DB: %w", err)
	}

	if err := s.syncPlatformMappings(ctx); err != nil {
		return err
	}

	s.logger.Info("Alarm cache recovered from DB",
		slog.String("reason", reason),
		slog.String("missing_key", sharedalarmkeys.AlarmChannelRegistryKey),
	)

	return nil
}

func (s *RuntimeScheduler) syncPlatformMappings(ctx context.Context) error {
	if s.platformMappingSyncer == nil {
		return nil
	}
	if err := s.platformMappingSyncer.SyncPlatformMappings(ctx); err != nil {
		return fmt.Errorf("recover alarm cache: sync platform mappings: %w", err)
	}
	return nil
}

func (s *RuntimeScheduler) syncPlatformMappingsIfMissing(ctx context.Context) error {
	if !s.canSyncPlatformMappings() {
		return nil
	}

	for _, mapping := range alarmPlatformMappings() {
		synced, err := s.syncPlatformMappingIfMissing(ctx, mapping)
		if synced || err != nil {
			return err
		}
	}

	return nil
}

func (s *RuntimeScheduler) canSyncPlatformMappings() bool {
	return s != nil && s.cacheSvc != nil && s.platformMappingSyncer != nil
}

func (s *RuntimeScheduler) syncPlatformMappingIfMissing(ctx context.Context, mapping alarmPlatformMappingKeys) (bool, error) {
	missing, err := s.platformMappingMissing(ctx, mapping)
	if err != nil {
		return false, err
	}
	if !missing {
		return false, nil
	}

	return true, s.syncPlatformMappings(ctx)
}

type alarmPlatformMappingKeys struct {
	key            string
	emptyMarkerKey string
}

func alarmPlatformMappings() []alarmPlatformMappingKeys {
	return []alarmPlatformMappingKeys{
		{key: sharedalarmkeys.ChzzkChannelMapKey, emptyMarkerKey: sharedalarmkeys.ChzzkChannelMapEmptyKey},
		{key: sharedalarmkeys.TwitchLoginMapKey, emptyMarkerKey: sharedalarmkeys.TwitchLoginMapEmptyKey},
		{key: sharedalarmkeys.TwitchChannelLoginMapKey, emptyMarkerKey: sharedalarmkeys.TwitchChannelLoginMapEmptyKey},
	}
}

func (s *RuntimeScheduler) platformMappingMissing(ctx context.Context, mapping alarmPlatformMappingKeys) (bool, error) {
	exists, err := s.cacheSvc.Exists(ctx, mapping.key)
	if err != nil {
		return false, fmt.Errorf("recover alarm cache: check platform mapping %s: %w", mapping.key, err)
	}
	if exists {
		return false, nil
	}

	empty, err := s.cacheSvc.Exists(ctx, mapping.emptyMarkerKey)
	if err != nil {
		return false, fmt.Errorf("recover alarm cache: check platform mapping empty marker %s: %w", mapping.emptyMarkerKey, err)
	}

	return !empty, nil
}

func (s *RuntimeScheduler) recoverAlarmCacheAfterCheckFailure(ctx context.Context, checkErr error) error {
	if s == nil || s.cacheSvc == nil || checkErr == nil || !isCacheFailure(checkErr) {
		return nil
	}

	readyCtx, cancel := context.WithTimeout(ctx, alarmCacheRecoveryTimeout)
	defer cancel()

	if err := s.cacheSvc.WaitUntilReady(readyCtx, alarmCacheRecoveryTimeout); err != nil {
		return fmt.Errorf("recover alarm cache after check failure: wait cache ready: %w", err)
	}

	return s.recoverAlarmCacheIfRegistryEmpty(readyCtx, "youtube_check_cache_error")
}

func alarmPlatformMappingSyncerFrom(alarmCRUD domain.AlarmCRUD) alarmPlatformMappingSyncer {
	syncer, ok := alarmCRUD.(alarmPlatformMappingSyncer)
	if !ok {
		return nil
	}

	return syncer
}

func isCacheFailure(err error) bool {
	var cacheErr *cache.CacheError
	return errors.As(err, &cacheErr)
}
