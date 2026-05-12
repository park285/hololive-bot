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

package poller

import (
	"context"
	"fmt"
	"log/slog"

	"gorm.io/gorm"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox"
	trackingrepo "github.com/kapu/hololive-shared/pkg/service/youtube/tracking"
)

const pollerBatchMaxSize = 50

type batchRepository interface {
	PersistVideos(ctx context.Context, videos []*domain.YouTubeVideo, notifications []*domain.YouTubeNotificationOutbox, trackingRows []*domain.YouTubeContentAlarmTracking, watermark *domain.YouTubeContentWatermark) error
	PersistCommunityPosts(ctx context.Context, posts []*domain.YouTubeCommunityPost, notifications []*domain.YouTubeNotificationOutbox, trackingRows []*domain.YouTubeContentAlarmTracking, watermark *domain.YouTubeContentWatermark) error
}

type gormBatchRepository struct {
	db *gorm.DB
}

func newBatchRepository(db *gorm.DB) batchRepository {
	return &gormBatchRepository{db: db}
}

func (r *gormBatchRepository) PersistVideos(ctx context.Context, videos []*domain.YouTubeVideo, notifications []*domain.YouTubeNotificationOutbox, trackingRows []*domain.YouTubeContentAlarmTracking, watermark *domain.YouTubeContentWatermark) error {
	if err := validateShortNotificationPublishedAt(videos, notifications); err != nil {
		return fmt.Errorf("validate short notifications: %w", err)
	}

	if err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
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
		if err := r.batchInsertNotifications(ctx, tx, notifications); err != nil {
			return fmt.Errorf("batch insert notifications: %w", err)
		}
		if err := reconcileTrackingRowsWithPersistedSendState(ctx, tx, trackingRows); err != nil {
			return fmt.Errorf("reconcile tracking rows with persisted send state: %w", err)
		}
		if err := trackingrepo.NewRepository(tx).UpsertBatch(ctx, trackingRows); err != nil {
			return fmt.Errorf("upsert video tracking: %w", err)
		}
		alarmStates := buildCommunityShortsAlarmStates(trackingRows)
		if err := trackingrepo.NewRepository(tx).UpsertAlarmStateBatch(ctx, alarmStates); err != nil {
			return fmt.Errorf("upsert short alarm states: %w", err)
		}
		if err := r.upsertWatermark(ctx, tx, watermark); err != nil {
			return fmt.Errorf("upsert watermark: %w", err)
		}
		return nil
	}); err != nil {
		return fmt.Errorf("persist videos transaction: %w", err)
	}
	r.persistLatencyClassificationsAfterCommit(ctx, trackingRows)
	return nil
}

func (r *gormBatchRepository) PersistCommunityPosts(ctx context.Context, posts []*domain.YouTubeCommunityPost, notifications []*domain.YouTubeNotificationOutbox, trackingRows []*domain.YouTubeContentAlarmTracking, watermark *domain.YouTubeContentWatermark) error {
	if err := validateCommunityNotificationPublishedAt(posts, notifications); err != nil {
		return fmt.Errorf("validate community notifications: %w", err)
	}

	if err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := r.batchUpsertCommunityPosts(ctx, tx, posts); err != nil {
			return fmt.Errorf("batch upsert community posts: %w", err)
		}
		sourcePosts := buildCommunitySourcePosts(posts, trackingRows)
		if err := trackingrepo.NewRepository(tx).UpsertSourcePostsBatch(ctx, sourcePosts); err != nil {
			return fmt.Errorf("upsert community source posts: %w", err)
		}
		if err := r.batchInsertNotifications(ctx, tx, notifications); err != nil {
			return fmt.Errorf("batch insert notifications: %w", err)
		}
		if err := reconcileTrackingRowsWithPersistedSendState(ctx, tx, trackingRows); err != nil {
			return fmt.Errorf("reconcile tracking rows with persisted send state: %w", err)
		}
		if err := trackingrepo.NewRepository(tx).UpsertBatch(ctx, trackingRows); err != nil {
			return fmt.Errorf("upsert community tracking: %w", err)
		}
		alarmStates := buildCommunityShortsAlarmStates(trackingRows)
		if err := trackingrepo.NewRepository(tx).UpsertAlarmStateBatch(ctx, alarmStates); err != nil {
			return fmt.Errorf("upsert community alarm states: %w", err)
		}
		if err := r.upsertWatermark(ctx, tx, watermark); err != nil {
			return fmt.Errorf("upsert watermark: %w", err)
		}
		return nil
	}); err != nil {
		return fmt.Errorf("persist community posts transaction: %w", err)
	}
	r.persistLatencyClassificationsAfterCommit(ctx, trackingRows)
	return nil
}

func (r *gormBatchRepository) persistLatencyClassificationsAfterCommit(
	ctx context.Context,
	trackingRows []*domain.YouTubeContentAlarmTracking,
) {
	if r == nil || r.db == nil || len(trackingRows) == 0 {
		return
	}

	identities := make([]outbox.PostTrackingIdentity, 0, len(trackingRows))
	for i := range trackingRows {
		if trackingRows[i] == nil {
			continue
		}
		identities = append(identities, outbox.PostTrackingIdentity{
			Kind:      trackingRows[i].Kind,
			ContentID: trackingRows[i].ContentID,
		})
	}
	if len(identities) == 0 {
		return
	}

	if err := outbox.NewDeliveryTelemetryRepository(r.db).PersistPostLatencyClassificationsByIdentities(ctx, identities); err != nil {
		slog.Default().Warn("Failed to persist post latency classifications after detection commit",
			slog.Int("tracking_rows", len(identities)),
			slog.Any("error", err),
		)
	}
}
