package poller

import (
	"context"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/kapu/hololive-shared/pkg/domain"
)

const pollerBatchMaxSize = 50

type batchRepository interface {
	PersistVideos(ctx context.Context, videos []*domain.YouTubeVideo, notifications []*domain.YouTubeNotificationOutbox, watermark *domain.YouTubeContentWatermark) error
	PersistCommunityPosts(ctx context.Context, posts []*domain.YouTubeCommunityPost, notifications []*domain.YouTubeNotificationOutbox, watermark *domain.YouTubeContentWatermark) error
}

type gormBatchRepository struct {
	db *gorm.DB
}

func newBatchRepository(db *gorm.DB) batchRepository {
	return &gormBatchRepository{db: db}
}

func (r *gormBatchRepository) PersistVideos(ctx context.Context, videos []*domain.YouTubeVideo, notifications []*domain.YouTubeNotificationOutbox, watermark *domain.YouTubeContentWatermark) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := r.batchUpsertVideos(ctx, tx, videos); err != nil {
			return fmt.Errorf("batch upsert videos: %w", err)
		}
		if err := r.batchInsertNotifications(ctx, tx, notifications); err != nil {
			return fmt.Errorf("batch insert notifications: %w", err)
		}
		if err := r.upsertWatermark(ctx, tx, watermark); err != nil {
			return fmt.Errorf("upsert watermark: %w", err)
		}
		return nil
	})
}

func (r *gormBatchRepository) PersistCommunityPosts(ctx context.Context, posts []*domain.YouTubeCommunityPost, notifications []*domain.YouTubeNotificationOutbox, watermark *domain.YouTubeContentWatermark) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := r.batchUpsertCommunityPosts(ctx, tx, posts); err != nil {
			return fmt.Errorf("batch upsert community posts: %w", err)
		}
		if err := r.batchInsertNotifications(ctx, tx, notifications); err != nil {
			return fmt.Errorf("batch insert notifications: %w", err)
		}
		if err := r.upsertWatermark(ctx, tx, watermark); err != nil {
			return fmt.Errorf("upsert watermark: %w", err)
		}
		return nil
	})
}

func (r *gormBatchRepository) batchUpsertVideos(ctx context.Context, tx *gorm.DB, videos []*domain.YouTubeVideo) error {
	if len(videos) == 0 {
		return nil
	}

	for start := 0; start < len(videos); start += pollerBatchMaxSize {
		end := start + pollerBatchMaxSize
		if end > len(videos) {
			end = len(videos)
		}
		chunk := videos[start:end]
		if err := r.upsertVideosChunk(ctx, tx, chunk); err != nil {
			return err
		}
	}

	return nil
}

func (r *gormBatchRepository) upsertVideosChunk(ctx context.Context, tx *gorm.DB, videos []*domain.YouTubeVideo) error {
	now := time.Now()
	args := make([]any, 0, len(videos)*11)
	var sb strings.Builder
	sb.WriteString(`
		INSERT INTO youtube_videos
			(video_id, channel_id, title, thumbnail, duration, published_text, is_short, is_live_replay, view_count, first_seen_at, last_seen_at)
		VALUES
	`)

	for i, video := range videos {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString("(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)")
		args = append(
			args,
			video.VideoID,
			video.ChannelID,
			video.Title,
			video.Thumbnail,
			video.Duration,
			video.PublishedText,
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
		end := start + pollerBatchMaxSize
		if end > len(posts) {
			end = len(posts)
		}
		chunk := posts[start:end]
		if err := r.upsertCommunityPostsChunk(ctx, tx, chunk); err != nil {
			return err
		}
	}

	return nil
}

func (r *gormBatchRepository) upsertCommunityPostsChunk(ctx context.Context, tx *gorm.DB, posts []*domain.YouTubeCommunityPost) error {
	now := time.Now()
	args := make([]any, 0, len(posts)*12)
	var sb strings.Builder
	sb.WriteString(`
		INSERT INTO youtube_community_posts
			(post_id, channel_id, author_name, author_photo, content_text, published_text, like_count, comment_count, images, attached_video, first_seen_at, last_seen_at)
		VALUES
	`)

	for i, post := range posts {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString("(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)")
		args = append(
			args,
			post.PostID,
			post.ChannelID,
			post.AuthorName,
			post.AuthorPhoto,
			post.ContentText,
			post.PublishedText,
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
		end := start + pollerBatchMaxSize
		if end > len(notifications) {
			end = len(notifications)
		}
		chunk := notifications[start:end]
		if err := r.insertNotificationsChunk(ctx, tx, chunk); err != nil {
			return err
		}
	}

	return nil
}

func (r *gormBatchRepository) insertNotificationsChunk(ctx context.Context, tx *gorm.DB, notifications []*domain.YouTubeNotificationOutbox) error {
	now := time.Now()
	args := make([]any, 0, len(notifications)*8)
	var sb strings.Builder
	sb.WriteString(`
		INSERT INTO youtube_notification_outbox
			(kind, channel_id, content_id, payload, status, attempt_count, next_attempt_at, created_at)
		VALUES
	`)

	for i, notification := range notifications {
		if i > 0 {
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
		args = append(
			args,
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

	sb.WriteString(`
		ON CONFLICT (kind, content_id) DO NOTHING
	`)

	if err := tx.WithContext(ctx).Exec(sb.String(), args...).Error; err != nil {
		return fmt.Errorf("exec notification insert chunk (%d rows): %w", len(notifications), err)
	}

	return nil
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
