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

package batchrepo

import (
	"context"
	"fmt"
	"log/slog"

	"gorm.io/gorm"

	"github.com/kapu/hololive-shared/pkg/domain"
	trackingrepo "github.com/kapu/hololive-shared/pkg/service/youtube/tracking"
)

const PollerBatchMaxSize = 50

type BatchRepository interface {
	PersistVideos(ctx context.Context, videos []*domain.YouTubeVideo, notifications []*domain.YouTubeNotificationOutbox, trackingRows []*domain.YouTubeContentAlarmTracking, watermark *domain.YouTubeContentWatermark) error
	PersistCommunityPosts(ctx context.Context, posts []*domain.YouTubeCommunityPost, notifications []*domain.YouTubeNotificationOutbox, trackingRows []*domain.YouTubeContentAlarmTracking, watermark *domain.YouTubeContentWatermark) error
}

type GormBatchRepository struct {
	DB               *gorm.DB
	latencyPersister PostLatencyClassificationPersister
}

func NewBatchRepository(db *gorm.DB) BatchRepository {
	return &GormBatchRepository{DB: db}
}

func NewGormBatchRepository(db *gorm.DB) *GormBatchRepository {
	return &GormBatchRepository{DB: db}
}

func NewGormBatchRepositoryWithPersister(db *gorm.DB, persister PostLatencyClassificationPersister) *GormBatchRepository {
	return &GormBatchRepository{DB: db, latencyPersister: persister}
}

func (r *GormBatchRepository) PersistVideos(ctx context.Context, videos []*domain.YouTubeVideo, notifications []*domain.YouTubeNotificationOutbox, trackingRows []*domain.YouTubeContentAlarmTracking, watermark *domain.YouTubeContentWatermark) error {
	if err := validateShortNotificationPublishedAt(videos, notifications); err != nil {
		return fmt.Errorf("validate short notifications: %w", err)
	}

	if err := r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return r.persistVideosTx(ctx, tx, videos, notifications, trackingRows, watermark)
	}); err != nil {
		return fmt.Errorf("persist videos transaction: %w", err)
	}
	r.persistLatencyClassificationsAfterCommit(ctx, trackingRows)
	return nil
}

func (r *GormBatchRepository) PersistCommunityPosts(ctx context.Context, posts []*domain.YouTubeCommunityPost, notifications []*domain.YouTubeNotificationOutbox, trackingRows []*domain.YouTubeContentAlarmTracking, watermark *domain.YouTubeContentWatermark) error {
	if err := validateCommunityNotificationPublishedAt(posts, notifications); err != nil {
		return fmt.Errorf("validate community notifications: %w", err)
	}

	if err := r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return r.persistCommunityPostsTx(ctx, tx, posts, notifications, trackingRows, watermark)
	}); err != nil {
		return fmt.Errorf("persist community posts transaction: %w", err)
	}
	r.persistLatencyClassificationsAfterCommit(ctx, trackingRows)
	return nil
}

func (r *GormBatchRepository) persistVideosTx(
	ctx context.Context,
	tx *gorm.DB,
	videos []*domain.YouTubeVideo,
	notifications []*domain.YouTubeNotificationOutbox,
	trackingRows []*domain.YouTubeContentAlarmTracking,
	watermark *domain.YouTubeContentWatermark,
) error {
	if err := r.batchUpsertVideos(ctx, tx, videos); err != nil {
		return fmt.Errorf("batch upsert videos: %w", err)
	}
	if err := r.resolveShortPersistedContentIDs(ctx, tx, notifications, trackingRows); err != nil {
		return fmt.Errorf("resolve short persisted content ids: %w", err)
	}
	sourcePosts := buildShortSourcePosts(videos, trackingRows)
	if err := trackingrepo.NewRepository(tx).UpsertSourcePostsBatch(ctx, sourcePosts); err != nil {
		return fmt.Errorf("upsert short source posts: %w", err)
	}
	return r.persistTrackingAndWatermark(ctx, tx, notifications, trackingRows, watermark, "video", "short")
}

func (r *GormBatchRepository) persistCommunityPostsTx(
	ctx context.Context,
	tx *gorm.DB,
	posts []*domain.YouTubeCommunityPost,
	notifications []*domain.YouTubeNotificationOutbox,
	trackingRows []*domain.YouTubeContentAlarmTracking,
	watermark *domain.YouTubeContentWatermark,
) error {
	if err := r.batchUpsertCommunityPosts(ctx, tx, posts); err != nil {
		return fmt.Errorf("batch upsert community posts: %w", err)
	}
	sourcePosts := buildCommunitySourcePosts(posts, trackingRows)
	if err := trackingrepo.NewRepository(tx).UpsertSourcePostsBatch(ctx, sourcePosts); err != nil {
		return fmt.Errorf("upsert community source posts: %w", err)
	}
	return r.persistTrackingAndWatermark(ctx, tx, notifications, trackingRows, watermark, "community", "community")
}

func (r *GormBatchRepository) persistTrackingAndWatermark(
	ctx context.Context,
	tx *gorm.DB,
	notifications []*domain.YouTubeNotificationOutbox,
	trackingRows []*domain.YouTubeContentAlarmTracking,
	watermark *domain.YouTubeContentWatermark,
	trackingLabel string,
	alarmStateLabel string,
) error {
	if err := r.BatchInsertNotifications(ctx, tx, notifications); err != nil {
		return fmt.Errorf("batch insert notifications: %w", err)
	}
	if err := reconcileTrackingRowsWithPersistedSendState(ctx, tx, trackingRows); err != nil {
		return fmt.Errorf("reconcile tracking rows with persisted send state: %w", err)
	}
	if err := trackingrepo.NewRepository(tx).UpsertBatch(ctx, trackingRows); err != nil {
		return fmt.Errorf("upsert %s tracking: %w", trackingLabel, err)
	}
	alarmStates := buildCommunityShortsAlarmStates(trackingRows)
	if err := trackingrepo.NewRepository(tx).UpsertAlarmStateBatch(ctx, alarmStates); err != nil {
		return fmt.Errorf("upsert %s alarm states: %w", alarmStateLabel, err)
	}
	if err := r.upsertWatermark(ctx, tx, watermark); err != nil {
		return fmt.Errorf("upsert watermark: %w", err)
	}
	return nil
}

func (r *GormBatchRepository) persistLatencyClassificationsAfterCommit(
	ctx context.Context,
	trackingRows []*domain.YouTubeContentAlarmTracking,
) {
	if r == nil || r.DB == nil || r.latencyPersister == nil || len(trackingRows) == 0 {
		return
	}

	identities := make([]LatencyClassificationIdentity, 0, len(trackingRows))
	for i := range trackingRows {
		if trackingRows[i] == nil {
			continue
		}
		identities = append(identities, LatencyClassificationIdentity{
			Kind:      trackingRows[i].Kind,
			ContentID: trackingRows[i].ContentID,
		})
	}
	if len(identities) == 0 {
		return
	}

	if err := r.latencyPersister.PersistPostLatencyClassificationsByIdentities(ctx, identities); err != nil {
		slog.Default().Warn("Failed to persist post latency classifications after detection commit",
			slog.Int("tracking_rows", len(identities)),
			slog.Any("error", err),
		)
	}
}
