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

package holodexprovider

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/park285/shared-go/v2/pkg/retry"
	"github.com/park285/shared-go/v2/pkg/runtime/lifecycle"

	"github.com/kapu/hololive-shared/pkg/service/member"
)

const (
	// 프로필 사진은 자주 바뀌지 않으므로 주기와 재동기화 기준을 같은 7일로 둔다.
	photoSyncInterval       = 7 * 24 * time.Hour
	photoSyncStaleThreshold = 7 * 24 * time.Hour
	// 기동 직후 다른 Holodex 호출과 겹치지 않도록 첫 동기화를 미룬다.
	photoSyncInitialDelay = 10 * time.Second
)

type PhotoSyncService struct {
	holodex          *Service
	memberRepository photoSyncMemberRepository
	logger           *slog.Logger

	syncInterval   time.Duration
	staleThreshold time.Duration // 이 기간 이상 지난 photo는 재동기화한다.
}

type photoSyncMemberRepository interface {
	GetAllChannelIDs(ctx context.Context) ([]string, error)
	GetMembersNeedingPhotoSync(ctx context.Context, staleThreshold time.Duration) ([]string, error)
	UpdatePhoto(ctx context.Context, channelID, photoURL string) error
}

func NewPhotoSyncService(
	holodex *Service,
	memberRepository *member.Repository,
	logger *slog.Logger,
) *PhotoSyncService {
	return &PhotoSyncService{
		holodex:          holodex,
		memberRepository: memberRepository,
		logger:           logger.With(slog.String("service", "photo_sync")),
		syncInterval:     photoSyncInterval,
		staleThreshold:   photoSyncStaleThreshold,
	}
}

func (ps *PhotoSyncService) Start(ctx context.Context) {
	ps.logger.Info("Starting photo sync service",
		slog.Duration("interval", ps.syncInterval),
		slog.Duration("stale_threshold", ps.staleThreshold),
	)

	if !ps.waitBeforeInitialSync(ctx) {
		return
	}

	ps.syncWithRetry(ctx, photoSyncRetryOptions())
	ps.runPeriodicSync(ctx)
}

func (ps *PhotoSyncService) waitBeforeInitialSync(ctx context.Context) bool {
	ps.logger.Debug("Waiting before initial sync to avoid API contention",
		slog.Duration("delay", photoSyncInitialDelay),
	)

	return retry.Sleep(ctx, photoSyncInitialDelay)
}

func (ps *PhotoSyncService) runPeriodicSync(ctx context.Context) {
	if err := lifecycle.RunTickerLoop(ctx, ps.syncInterval, func(ctx context.Context) error {
		ps.syncWithRetry(ctx, photoSyncRetryOptions())

		return nil
	}); err != nil && ctx.Err() != nil {
		ps.logger.Info("Photo sync service stopped")
	}
}

func photoSyncRetryOptions() retry.RetryOptions {
	return retry.RetryOptions{
		MaxAttempts: 3,
		BaseDelay:   5 * time.Second,
		Jitter:      2 * time.Second,
	}
}

// syncWithRetry는 중간 실패를 재시도 직전에 Warn으로 남기고, 시도를 모두 쓴 최종 실패만 Error로 한 번 남긴다.
// 호출자 context가 취소돼 끝난 경우는 종료 경로이므로 Error를 남기지 않는다.
func (ps *PhotoSyncService) syncWithRetry(ctx context.Context, options retry.RetryOptions) {
	options.OnRetry = func(attempt int, err error, _ time.Duration) {
		ps.logger.Warn("Photo sync failed, will retry",
			slog.Any("error", err),
			slog.Int("attempt", attempt),
			slog.Int("max_attempts", options.MaxAttempts),
		)
	}

	lastErr := retry.WithRetry(ctx, options, func(ctx context.Context) error {
		return ps.doSync(ctx, false)
	})
	if lastErr == nil || ctx.Err() != nil {
		return
	}

	ps.logger.Error("Photo sync failed after all retries",
		slog.Any("error", lastErr),
		slog.Int("max_attempts", options.MaxAttempts),
	)
}

func (ps *PhotoSyncService) SyncAll(ctx context.Context) error {
	ps.logger.Info("Starting full photo sync")

	if err := ps.doSync(ctx, true); err != nil {
		return fmt.Errorf("do sync: %w", err)
	}

	return nil
}

func (ps *PhotoSyncService) doSync(ctx context.Context, forceAll bool) error {
	channelIDs, err := ps.photoSyncChannelIDs(ctx, forceAll)
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

	photoMap, err := ps.fetchPhotoMap(ctx)
	if err != nil {
		return fmt.Errorf("fetch photo map: %w", err)
	}

	successCount, failCount := ps.updateMemberPhotos(ctx, channelIDs, photoMap)
	ps.logger.Info("Photo sync completed",
		slog.Int("success", successCount),
		slog.Int("failed", failCount),
		slog.Int("total", len(channelIDs)),
	)

	return nil
}

func (ps *PhotoSyncService) photoSyncChannelIDs(ctx context.Context, forceAll bool) ([]string, error) {
	if forceAll {
		out, err := ps.memberRepository.GetAllChannelIDs(ctx)
		if err != nil {
			return out, fmt.Errorf("get all channel IDs: %w", err)
		}

		return out, nil
	}

	out, err := ps.memberRepository.GetMembersNeedingPhotoSync(ctx, ps.staleThreshold)
	if err != nil {
		return out, fmt.Errorf("get members needing photo sync: %w", err)
	}

	return out, nil
}

func (ps *PhotoSyncService) fetchPhotoMap(ctx context.Context) (map[string]string, error) {
	allChannels, err := ps.holodex.fetchHololiveChannelList(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetch Hololive channel list: %w", err)
	}

	photoMap := make(map[string]string, len(allChannels))
	for _, ch := range allChannels {
		if ch.Photo != nil && *ch.Photo != "" {
			highResPhoto := member.UpgradePhotoResolution(*ch.Photo)

			photoMap[ch.ID] = highResPhoto
		}
	}

	return photoMap, nil
}

func (ps *PhotoSyncService) updateMemberPhotos(ctx context.Context, channelIDs []string, photoMap map[string]string) (int, int) {
	successCount := 0
	failCount := 0

	for _, channelID := range channelIDs {
		photo, exists := photoMap[channelID]
		if !ps.hasPhotoForChannel(channelID, exists, photo) {
			continue
		}

		if err := ps.memberRepository.UpdatePhoto(ctx, channelID, photo); err != nil {
			ps.logger.Warn("Failed to update photo",
				slog.String("channel_id", channelID),
				slog.Any("error", err),
			)

			failCount++

			continue
		}

		successCount++
	}

	return successCount, failCount
}

func (ps *PhotoSyncService) hasPhotoForChannel(channelID string, exists bool, photo string) bool {
	if exists && photo != "" {
		return true
	}

	ps.logger.Debug("No photo found for channel",
		slog.String("channel_id", channelID),
	)

	return false
}
