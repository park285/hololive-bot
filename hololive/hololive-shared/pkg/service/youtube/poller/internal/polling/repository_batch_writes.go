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

package polling

import (
	"context"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/kapu/hololive-shared/pkg/domain"
	yttimestamp "github.com/kapu/hololive-shared/pkg/service/youtube/timestamp"
)

func (r *gormBatchRepository) batchUpsertVideos(ctx context.Context, tx *gorm.DB, videos []*domain.YouTubeVideo) error {
	if len(videos) == 0 {
		return nil
	}

	for start := 0; start < len(videos); start += pollerBatchMaxSize {
		end := min(start+pollerBatchMaxSize, len(videos))
		chunk := videos[start:end]
		if err := r.upsertVideosChunk(ctx, tx, chunk); err != nil {
			return err
		}
	}

	return nil
}

func (r *gormBatchRepository) upsertVideosChunk(ctx context.Context, tx *gorm.DB, videos []*domain.YouTubeVideo) error {
	now := time.Now()
	args := make([]any, 0, len(videos)*12)
	var sb strings.Builder
	sb.WriteString(`
		INSERT INTO youtube_videos
			(video_id, channel_id, title, thumbnail, duration, published_text, published_at, is_short, is_live_replay, view_count, first_seen_at, last_seen_at)
		VALUES
	`)

	for i, video := range videos {
		publishedAt := yttimestamp.NormalizePtr(video.PublishedAt)
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString("(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)")
		args = append(
			args,
			video.VideoID,
			video.ChannelID,
			video.Title,
			video.Thumbnail,
			video.Duration,
			video.PublishedText,
			publishedAt,
			video.IsShort,
			video.IsLiveReplay,
			video.ViewCount,
			now,
			now,
		)
	}

	sb.WriteString(`
		ON CONFLICT (video_id) DO UPDATE
		SET last_seen_at = EXCLUDED.last_seen_at,
		    published_at = COALESCE(youtube_videos.published_at, EXCLUDED.published_at),
		    view_count = EXCLUDED.view_count
	`)

	if err := tx.WithContext(ctx).Exec(sb.String(), args...).Error; err != nil {
		return fmt.Errorf("exec video upsert chunk (%d rows): %w", len(videos), err)
	}

	return nil
}

func (r *gormBatchRepository) batchUpsertCommunityPosts(ctx context.Context, tx *gorm.DB, posts []*domain.YouTubeCommunityPost) error {
	if len(posts) == 0 {
		return nil
	}

	for start := 0; start < len(posts); start += pollerBatchMaxSize {
		end := min(start+pollerBatchMaxSize, len(posts))
		chunk := posts[start:end]
		if err := r.upsertCommunityPostsChunk(ctx, tx, chunk); err != nil {
			return err
		}
	}

	return nil
}

func (r *gormBatchRepository) upsertCommunityPostsChunk(ctx context.Context, tx *gorm.DB, posts []*domain.YouTubeCommunityPost) error {
	now := time.Now()
	args := make([]any, 0, len(posts)*13)
	var sb strings.Builder
	sb.WriteString(`
		INSERT INTO youtube_community_posts
			(post_id, channel_id, author_name, author_photo, content_text, published_text, published_at, like_count, comment_count, images, attached_video, first_seen_at, last_seen_at)
		VALUES
	`)

	for i, post := range posts {
		publishedAt := yttimestamp.NormalizePtr(post.PublishedAt)
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString("(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)")
		args = append(
			args,
			post.PostID,
			post.ChannelID,
			post.AuthorName,
			post.AuthorPhoto,
			post.ContentText,
			post.PublishedText,
			publishedAt,
			post.LikeCount,
			post.CommentCount,
			post.Images,
			post.AttachedVideo,
			now,
			now,
		)
	}

	sb.WriteString(`
		ON CONFLICT (post_id) DO UPDATE
		SET last_seen_at = EXCLUDED.last_seen_at,
		    published_at = COALESCE(youtube_community_posts.published_at, EXCLUDED.published_at),
		    like_count = EXCLUDED.like_count,
		    comment_count = EXCLUDED.comment_count
	`)

	if err := tx.WithContext(ctx).Exec(sb.String(), args...).Error; err != nil {
		return fmt.Errorf("exec community post upsert chunk (%d rows): %w", len(posts), err)
	}

	return nil
}

func (r *gormBatchRepository) batchInsertNotifications(ctx context.Context, tx *gorm.DB, notifications []*domain.YouTubeNotificationOutbox) error {
	if len(notifications) == 0 {
		return nil
	}

	for start := 0; start < len(notifications); start += pollerBatchMaxSize {
		end := min(start+pollerBatchMaxSize, len(notifications))
		chunk := notifications[start:end]
		if err := r.insertNotificationsChunk(ctx, tx, chunk); err != nil {
			return err
		}
	}

	return nil
}

func (r *gormBatchRepository) insertNotificationsChunk(ctx context.Context, tx *gorm.DB, notifications []*domain.YouTubeNotificationOutbox) error {
	completedSentAtByIdentity, reactivationRows, err := prepareNotificationInsertChunk(ctx, tx, notifications)
	if err != nil {
		return err
	}

	activeNotifications := filterCompletedNotifications(notifications, completedSentAtByIdentity)
	if len(activeNotifications) == 0 {
		return nil
	}

	now := time.Now()
	args := make([]any, 0, len(activeNotifications)*8)
	var sb strings.Builder
	sb.WriteString(`
		INSERT INTO youtube_notification_outbox
			(kind, channel_id, content_id, payload, status, attempt_count, next_attempt_at, created_at)
		VALUES
	`)

	for i, notification := range activeNotifications {
		appendNotificationInsertArgs(&sb, &args, i, notification, now)
	}

	sb.WriteString(`
		ON CONFLICT (kind, content_id) DO UPDATE
		SET channel_id = EXCLUDED.channel_id,
		    payload = EXCLUDED.payload,
		    status = 'PENDING',
		    attempt_count = 0,
		    next_attempt_at = EXCLUDED.next_attempt_at,
		    locked_at = NULL,
		    sent_at = NULL,
		    error = ''
		WHERE youtube_notification_outbox.status = 'FAILED'
		  AND youtube_notification_outbox.kind IN ('COMMUNITY_POST', 'NEW_SHORT')
	`)

	if err := tx.WithContext(ctx).Exec(sb.String(), args...).Error; err != nil {
		return fmt.Errorf("exec notification insert chunk (%d rows): %w", len(activeNotifications), err)
	}
	if err := rearmFailedDeliveryRows(ctx, tx, collectFailedNotificationOutboxIDs(reactivationRows), now); err != nil {
		return fmt.Errorf("rearm failed delivery rows: %w", err)
	}

	return nil
}

func prepareNotificationInsertChunk(
	ctx context.Context,
	tx *gorm.DB,
	notifications []*domain.YouTubeNotificationOutbox,
) (map[string]time.Time, []failedNotificationOutboxRow, error) {
	if err := validateNotificationDedupeKeys(notifications); err != nil {
		return nil, nil, err
	}

	completedSentAtByIdentity, err := loadCompletedNotificationSentAtByIdentity(ctx, tx, notifications)
	if err != nil {
		return nil, nil, fmt.Errorf("load completed notification sent state: %w", err)
	}
	failedRows, err := loadFailedNotificationOutboxRows(ctx, tx, notifications)
	if err != nil {
		return nil, nil, fmt.Errorf("load failed outbox rows: %w", err)
	}

	completedFailedRows, reactivationRows := partitionFailedNotificationOutboxRows(failedRows, completedSentAtByIdentity)
	if err := finalizeCompletedFailedNotificationRows(ctx, tx, completedFailedRows, completedSentAtByIdentity); err != nil {
		return nil, nil, fmt.Errorf("finalize completed failed notification rows: %w", err)
	}
	return completedSentAtByIdentity, reactivationRows, nil
}

func validateNotificationDedupeKeys(notifications []*domain.YouTubeNotificationOutbox) error {
	for i, notification := range notifications {
		if _, err := notification.DedupeKey(); err != nil {
			return fmt.Errorf("validate notification dedupe key at index %d: %w", i, err)
		}
	}
	return nil
}

func appendNotificationInsertArgs(
	sb *strings.Builder,
	args *[]any,
	index int,
	notification *domain.YouTubeNotificationOutbox,
	now time.Time,
) {
	if index > 0 {
		sb.WriteByte(',')
	}

	status := notification.Status
	if status == "" {
		status = domain.OutboxStatusPending
	}
	nextAttemptAt := notification.NextAttemptAt
	if nextAttemptAt.IsZero() {
		nextAttemptAt = now
	}
	createdAt := notification.CreatedAt
	if createdAt.IsZero() {
		createdAt = now
	}

	sb.WriteString("(?, ?, ?, ?, ?, ?, ?, ?)")
	*args = append(
		*args,
		notification.Kind,
		notification.ChannelID,
		notification.ContentID,
		notification.Payload,
		status,
		notification.AttemptCount,
		nextAttemptAt,
		createdAt,
	)
}

func (r *gormBatchRepository) upsertWatermark(ctx context.Context, tx *gorm.DB, watermark *domain.YouTubeContentWatermark) error {
	if watermark == nil {
		return nil
	}

	now := time.Now()
	if err := tx.WithContext(ctx).Exec(`
		INSERT INTO youtube_content_watermarks
			(channel_id, watermark_type, initialized, last_content_id, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (channel_id, watermark_type) DO UPDATE
		SET initialized = EXCLUDED.initialized,
		    last_content_id = EXCLUDED.last_content_id,
		    updated_at = EXCLUDED.updated_at
	`,
		watermark.ChannelID,
		watermark.WatermarkType,
		watermark.Initialized,
		watermark.LastContentID,
		now,
	).Error; err != nil {
		return fmt.Errorf("exec watermark upsert: %w", err)
	}

	return nil
}
