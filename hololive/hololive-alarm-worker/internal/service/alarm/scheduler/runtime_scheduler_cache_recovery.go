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

	registryExists, err := s.cacheClient.Exists(ctx, sharedalarmkeys.AlarmChannelRegistryKey)
	if err != nil {
		return fmt.Errorf("recover alarm cache: check channel registry: %w", err)
	}

	if registryExists {
		return nil
	}

	emptyCache, err := s.cacheClient.Exists(ctx, sharedalarmkeys.AlarmSubscriberCacheEmptyKey)
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
		slog.String("missing_key", sharedalarmkeys.AlarmChannelRegistryKey),
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
