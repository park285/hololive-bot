package scheduler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	alarmkeys "github.com/kapu/hololive-alarm-worker/internal/service/alarm/keys"
	"github.com/kapu/hololive-shared/pkg/service/cache"
)

const (
	alarmCacheRecoveryInterval = time.Minute
	alarmCacheRecoveryTimeout  = 10 * time.Second
)

type alarmCacheWarmer interface {
	WarmCacheFromDB(ctx context.Context) error
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

			if err := ctx.Err(); err != nil {
				return fmt.Errorf("wait for cache recovery tick: %w", err)
			}

			return nil
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
	if !s.canRecoverAlarmCache() {
		return nil
	}

	registryExists, err := s.cacheClient.Exists(ctx, alarmkeys.AlarmChannelRegistryKey)
	if err != nil {
		return fmt.Errorf("recover alarm cache: check channel registry: %w", err)
	}

	if registryExists {
		return nil
	}

	emptyCache, err := s.cacheClient.Exists(ctx, alarmkeys.AlarmSubscriberCacheEmptyKey)
	if err != nil {
		return fmt.Errorf("recover alarm cache: check empty marker: %w", err)
	}

	if emptyCache {
		return nil
	}

	if err := s.recoverAlarmCacheFromDB(ctx, reason); err != nil {
		return fmt.Errorf("recover alarm cache from DB: %w", err)
	}

	return nil
}

func (s *RuntimeScheduler) canRecoverAlarmCache() bool {
	return s != nil && s.cacheClient != nil && s.alarmCacheWarmer != nil
}

func (s *RuntimeScheduler) recoverAlarmCacheFromDB(ctx context.Context, reason string) error {
	if err := s.alarmCacheWarmer.WarmCacheFromDB(ctx); err != nil {
		return fmt.Errorf("recover alarm cache: warm cache from DB: %w", err)
	}

	s.logger.Info("Alarm cache recovered from DB",
		slog.String("reason", reason),
		slog.String("missing_key", alarmkeys.AlarmChannelRegistryKey),
	)

	return nil
}

func (s *RuntimeScheduler) recoverAlarmCacheAfterCheckFailure(ctx context.Context, checkErr error) error {
	if s == nil || s.cacheClient == nil || checkErr == nil || !isCacheFailure(checkErr) {
		return nil
	}

	readyCtx, cancel := context.WithTimeout(ctx, alarmCacheRecoveryTimeout)
	defer cancel()

	if err := s.cacheClient.WaitUntilReady(readyCtx, alarmCacheRecoveryTimeout); err != nil {
		return fmt.Errorf("recover alarm cache after check failure: wait cache ready: %w", err)
	}

	if err := s.recoverAlarmCacheIfRegistryEmpty(readyCtx, "youtube_check_cache_error"); err != nil {
		return fmt.Errorf("recover alarm cache if registry empty: %w", err)
	}

	return nil
}

func isCacheFailure(err error) bool {
	_, ok := errors.AsType[*cache.CacheError](err)
	return ok
}
