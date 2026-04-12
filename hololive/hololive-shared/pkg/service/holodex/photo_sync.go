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

package holodex

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/kapu/hololive-shared/internal/ctxutil"
	"github.com/kapu/hololive-shared/internal/retry"
	"github.com/kapu/hololive-shared/pkg/service/member"
)

type PhotoSyncService struct {
	holodex    *Service
	memberRepo *member.Repository
	logger     *slog.Logger

	syncInterval   time.Duration // 동기화 주기 (기본: 24시간)
	staleThreshold time.Duration // 이 기간 이상 지난 photo는 재동기화 (기본: 24시간)
}

func NewPhotoSyncService(
	holodex *Service,
	memberRepo *member.Repository,
	logger *slog.Logger,
) *PhotoSyncService {
	return &PhotoSyncService{
		holodex:        holodex,
		memberRepo:     memberRepo,
		logger:         logger.With(slog.String("service", "photo_sync")),
		syncInterval:   7 * 24 * time.Hour, // 7일마다 동기화 (프로필은 자주 변하지 않음)
		staleThreshold: 7 * 24 * time.Hour, // 7일 이상 된 photo는 재동기화
	}
}

func (ps *PhotoSyncService) Start(ctx context.Context) {
	ps.logger.Info("Starting photo sync service",
		slog.Duration("interval", ps.syncInterval),
		slog.Duration("stale_threshold", ps.staleThreshold),
	)

	// 앱 시작 시 다른 서비스들과의 API 경합을 피하기 위해 10초 딜레이
	ps.logger.Debug("Waiting 10 seconds before initial sync to avoid API contention")
	select {
	case <-ctx.Done():
		return
	case <-time.After(10 * time.Second):
	}

	ps.syncWithRetry(ctx, 3)

	ticker := time.NewTicker(ps.syncInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			ps.logger.Info("Photo sync service stopped")
			return
		case <-ticker.C:
			ps.syncWithRetry(ctx, 3)
		}
	}
}

func (ps *PhotoSyncService) syncWithRetry(ctx context.Context, maxRetries int) {
	for attempt := 1; attempt <= maxRetries; attempt++ {
		err := ps.doSync(ctx, false)
		if err == nil {
			return
		}

		ps.logger.Warn("Photo sync failed, will retry",
			slog.Any("error", err),
			slog.Int("attempt", attempt),
			slog.Int("max_retries", maxRetries),
		)

		if attempt < maxRetries {
			delay := retry.ComputeBackoffDelay(attempt-1, 5*time.Second, 2*time.Second)
			if !ctxutil.SleepWithContext(ctx, delay) {
				return
			}
		}
	}

	ps.logger.Error("Photo sync failed after all retries", slog.Int("max_retries", maxRetries))
}

func (ps *PhotoSyncService) SyncAll(ctx context.Context) error {
	ps.logger.Info("Starting full photo sync")
	return ps.doSync(ctx, true)
}

func (ps *PhotoSyncService) doSync(ctx context.Context, forceAll bool) error {
	var channelIDs []string
	var err error

	if forceAll {
		channelIDs, err = ps.memberRepo.GetAllChannelIDs(ctx)
	} else {
		channelIDs, err = ps.memberRepo.GetMembersNeedingPhotoSync(ctx, ps.staleThreshold)
	}

	if err != nil {
		return fmt.Errorf("get members needing photo sync: %w", err)
	}

	if len(channelIDs) == 0 {
		ps.logger.Debug("No members need photo sync")
		return nil
	}

	ps.logger.Info("Syncing photos from Holodex",
		slog.Int("target_count", len(channelIDs)),
		slog.Bool("force_all", forceAll),
	)

	// Holodex에서 전체 채널 리스트 조회 (최적화된 단일 API 호출)
	allChannels, err := ps.holodex.fetchHololiveChannelList(ctx)
	if err != nil {
		return fmt.Errorf("failed to fetch Hololive channel list: %w", err)
	}

	photoMap := make(map[string]string, len(allChannels))
	for _, ch := range allChannels {
		if ch.Photo != nil && *ch.Photo != "" {
			// 고화질로 업그레이드 (=s800 → =s1024)
			highResPhoto := member.UpgradePhotoResolution(*ch.Photo)
			photoMap[ch.ID] = highResPhoto
		}
	}

	successCount := 0
	failCount := 0

	for _, channelID := range channelIDs {
		photo, exists := photoMap[channelID]
		if !exists || photo == "" {
			ps.logger.Debug("No photo found for channel",
				slog.String("channel_id", channelID),
			)
			continue
		}

		if err := ps.memberRepo.UpdatePhoto(ctx, channelID, photo); err != nil {
			ps.logger.Warn("Failed to update photo",
				slog.String("channel_id", channelID),
				slog.Any("error", err),
			)
			failCount++
			continue
		}

		successCount++
	}

	ps.logger.Info("Photo sync completed",
		slog.Int("success", successCount),
		slog.Int("failed", failCount),
		slog.Int("total", len(channelIDs)),
	)

	return nil
}

func (ps *PhotoSyncService) SetSyncInterval(d time.Duration) {
	ps.syncInterval = d
}

func (ps *PhotoSyncService) SetStaleThreshold(d time.Duration) {
	ps.staleThreshold = d
}
