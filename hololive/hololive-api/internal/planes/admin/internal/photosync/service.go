// Package photosync는 admin plane이 소유하는 members.photo Holodex PhotoSync다.
package photosync

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/park285/shared-go/v2/pkg/retry"
	"github.com/park285/shared-go/v2/pkg/runtime/lifecycle"

	holodexprovider "github.com/kapu/hololive-shared/pkg/service/holodex/provider"
)

const (
	// 프로필 사진은 자주 바뀌지 않으므로 주기와 재동기화 기준을 같은 7일로 둔다.
	syncInterval   = 7 * 24 * time.Hour
	staleThreshold = 7 * 24 * time.Hour
	// 기동 직후 다른 Holodex 호출과 겹치지 않도록 첫 동기화를 미룬다.
	initialSyncDelay = 10 * time.Second
)

type Service struct {
	photos  photoSource
	members memberPhotoStore
	logger  *slog.Logger

	syncInterval   time.Duration
	staleThreshold time.Duration // 이 기간 이상 지난 photo는 재동기화한다.
}

type photoSource interface {
	HololiveChannelPhotos(ctx context.Context) (map[string]string, error)
}

type memberPhotoStore interface {
	membersNeedingSync(ctx context.Context, staleThreshold time.Duration) ([]string, error)
	updatePhoto(ctx context.Context, channelID, photoURL string) error
}

// New는 pool의 members.photo를 holodex 채널 사진으로 갱신하는 Service를 만든다.
func New(holodex *holodexprovider.Service, pool *pgxpool.Pool, logger *slog.Logger) *Service {
	return &Service{
		photos:         holodex,
		members:        repository{pool: pool},
		logger:         logger.With(slog.String("service", "photo_sync")),
		syncInterval:   syncInterval,
		staleThreshold: staleThreshold,
	}
}

// Start는 ctx가 끝날 때까지 돌아오지 않는다.
func (s *Service) Start(ctx context.Context) {
	s.logger.Info("Starting photo sync service",
		slog.Duration("interval", s.syncInterval),
		slog.Duration("stale_threshold", s.staleThreshold),
	)

	if !s.waitBeforeInitialSync(ctx) {
		return
	}

	s.syncWithRetry(ctx, retryOptions())
	s.runPeriodicSync(ctx)
}

func (s *Service) waitBeforeInitialSync(ctx context.Context) bool {
	s.logger.Debug("Waiting before initial sync to avoid API contention",
		slog.Duration("delay", initialSyncDelay),
	)

	return retry.Sleep(ctx, initialSyncDelay)
}

func (s *Service) runPeriodicSync(ctx context.Context) {
	if err := lifecycle.RunTickerLoop(ctx, s.syncInterval, func(ctx context.Context) error {
		s.syncWithRetry(ctx, retryOptions())

		return nil
	}); err != nil && ctx.Err() != nil {
		s.logger.Info("Photo sync service stopped")
	}
}

// retryOptions는 PhotoSync 재시도 예외 계약(docs/current/services/hololive-api.md)의 한도다.
func retryOptions() retry.RetryOptions {
	return retry.RetryOptions{
		MaxAttempts: 3,
		BaseDelay:   5 * time.Second,
		Jitter:      2 * time.Second,
	}
}

// syncWithRetry는 중간 실패를 재시도 직전에 Warn으로 남기고, 시도를 모두 쓴 최종 실패만 Error로 한 번 남긴다.
// 호출자 context가 취소돼 끝난 경우는 종료 경로이므로 Error를 남기지 않는다.
func (s *Service) syncWithRetry(ctx context.Context, options retry.RetryOptions) {
	options.OnRetry = func(attempt int, err error, _ time.Duration) {
		s.logger.Warn("Photo sync failed, will retry",
			slog.Any("error", err),
			slog.Int("attempt", attempt),
			slog.Int("max_attempts", options.MaxAttempts),
		)
	}

	lastErr := retry.WithRetry(ctx, options, s.doSync)
	if lastErr == nil || ctx.Err() != nil {
		return
	}

	s.logger.Error("Photo sync failed after all retries",
		slog.Any("error", lastErr),
		slog.Int("max_attempts", options.MaxAttempts),
	)
}

func (s *Service) doSync(ctx context.Context) error {
	channelIDs, err := s.members.membersNeedingSync(ctx, s.staleThreshold)
	if err != nil {
		return fmt.Errorf("get members needing photo sync: %w", err)
	}

	if len(channelIDs) == 0 {
		s.logger.Debug("No members need photo sync")

		return nil
	}

	s.logger.Info("Syncing photos from Holodex",
		slog.Int("target_count", len(channelIDs)),
	)

	photos, err := s.photos.HololiveChannelPhotos(ctx)
	if err != nil {
		return fmt.Errorf("fetch hololive channel photos: %w", err)
	}

	successCount, failCount := s.updateMemberPhotos(ctx, channelIDs, photos)
	s.logger.Info("Photo sync completed",
		slog.Int("success", successCount),
		slog.Int("failed", failCount),
		slog.Int("total", len(channelIDs)),
	)

	return nil
}

// updateMemberPhotos는 행별 저장 실패를 세기만 하고 sync 실패로 올리지 않는다. 재시도 대상이 아니다.
func (s *Service) updateMemberPhotos(ctx context.Context, channelIDs []string, photos map[string]string) (int, int) {
	successCount := 0
	failCount := 0

	for _, channelID := range channelIDs {
		photo, exists := photos[channelID]
		if !exists {
			s.logger.Debug("No photo found for channel",
				slog.String("channel_id", channelID),
			)

			continue
		}

		if err := s.members.updatePhoto(ctx, channelID, upgradePhotoResolution(photo)); err != nil {
			s.logger.Warn("Failed to update photo",
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

// photoSizeSuffixes는 고해상도로 바꿀 YouTube 썸네일 크기 표기를 찾는 순서대로 둔다.
var photoSizeSuffixes = []string{"=s88", "=s240", "=s800", "=s176", "=s68"}

// upgradePhotoResolution은 처음 찾은 크기 표기 하나만 =s1024로 바꾼다.
func upgradePhotoResolution(photoURL string) string {
	for _, size := range photoSizeSuffixes {
		if strings.Contains(photoURL, size) {
			return strings.Replace(photoURL, size, "=s1024", 1)
		}
	}

	return photoURL
}
