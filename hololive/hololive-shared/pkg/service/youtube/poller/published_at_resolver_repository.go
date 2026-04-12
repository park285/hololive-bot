package poller

import (
	"context"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/kapu/hololive-shared/pkg/domain"
	yttimestamp "github.com/kapu/hololive-shared/pkg/service/youtube/timestamp"
	trackingrepo "github.com/kapu/hololive-shared/pkg/service/youtube/tracking"
	json "github.com/park285/llm-kakao-bots/shared-go/pkg/json"
)

type publishedAtResolverRepository struct {
	db        *gorm.DB
	batchRepo *gormBatchRepository
}

type publishedAtFinalizeResult struct {
	enqueued bool
	reason   string
}

func newPublishedAtResolverRepository(db *gorm.DB) *publishedAtResolverRepository {
	return &publishedAtResolverRepository{
		db:        db,
		batchRepo: &gormBatchRepository{db: db},
	}
}

func (r *publishedAtResolverRepository) FinalizePublishedAtAndMaybeEnqueue(
	ctx context.Context,
	candidate trackingrepo.PublishedAtResolutionCandidate,
	publishedAt time.Time,
	routeDecider NotificationRouteDecider,
) (publishedAtFinalizeResult, error) {
	if r == nil || r.db == nil {
		return publishedAtFinalizeResult{}, fmt.Errorf("finalize published_at: db is nil")
	}
	if publishedAt.IsZero() {
		return publishedAtFinalizeResult{}, fmt.Errorf("finalize published_at: published_at is empty")
	}

	normalizedPublishedAt := yttimestamp.Normalize(publishedAt)
	result := publishedAtFinalizeResult{}
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		txRepo := trackingrepo.NewRepository(tx)

		stateRow, err := txRepo.FindAlarmStateByPostID(ctx, candidate.Kind, candidate.PostID)
		if err != nil {
			return fmt.Errorf("load alarm state row: %w", err)
		}
		if stateRow != nil {
			if stateRow.AlarmSentAt != nil && !stateRow.AlarmSentAt.IsZero() {
				result.reason = "already_sent"
				return nil
			}
			if stateRow.AuthorizedAt != nil && !stateRow.AuthorizedAt.IsZero() {
				result.reason = "already_claimed"
				return nil
			}
		}

		trackingRow, err := txRepo.FindByIdentity(ctx, candidate.Kind, candidate.ContentID)
		if err != nil {
			return fmt.Errorf("load tracking row: %w", err)
		}
		if trackingRow != nil && trackingRow.AlarmSentAt != nil && !trackingRow.AlarmSentAt.IsZero() {
			result.reason = "already_sent"
			return nil
		}

		notification, reason, err := r.finalizeCandidateState(ctx, tx, txRepo, candidate, normalizedPublishedAt, routeDecider)
		if err != nil {
			return err
		}
		if reason != "" {
			result.reason = reason
		}
		if notification == nil {
			if err := txRepo.ClearPublishedAtRetryAfter(ctx, candidate.Kind, candidate.PostID); err != nil {
				return fmt.Errorf("clear published_at retry after: %w", err)
			}
			return nil
		}
		if err := r.batchRepo.batchInsertNotifications(ctx, tx, []*domain.YouTubeNotificationOutbox{notification}); err != nil {
			return fmt.Errorf("insert pending notification: %w", err)
		}
		if err := txRepo.ClearPublishedAtRetryAfter(ctx, candidate.Kind, candidate.PostID); err != nil {
			return fmt.Errorf("clear published_at retry after: %w", err)
		}
		result.enqueued = true
		if result.reason == "" {
			result.reason = "resolved"
		}

		return nil
	})
	if err != nil {
		return publishedAtFinalizeResult{}, err
	}
	return result, nil
}

func (r *publishedAtResolverRepository) finalizeCandidateState(
	ctx context.Context,
	tx *gorm.DB,
	txRepo *trackingrepo.GormRepository,
	candidate trackingrepo.PublishedAtResolutionCandidate,
	publishedAt time.Time,
	routeDecider NotificationRouteDecider,
) (*domain.YouTubeNotificationOutbox, string, error) {
	switch candidate.Kind {
	case domain.OutboxKindNewShort:
		return r.finalizeShort(ctx, tx, txRepo, candidate, publishedAt, routeDecider)
	case domain.OutboxKindCommunityPost:
		return r.finalizeCommunity(ctx, tx, txRepo, candidate, publishedAt, routeDecider)
	default:
		return nil, "", fmt.Errorf("finalize published_at: unsupported kind %s", candidate.Kind)
	}
}

func (r *publishedAtResolverRepository) finalizeShort(
	ctx context.Context,
	tx *gorm.DB,
	txRepo *trackingrepo.GormRepository,
	candidate trackingrepo.PublishedAtResolutionCandidate,
	publishedAt time.Time,
	routeDecider NotificationRouteDecider,
) (*domain.YouTubeNotificationOutbox, string, error) {
	videoID := normalizeShortVideoResourceID(candidate.PostID)
	if videoID == "" {
		videoID = normalizeShortVideoResourceID(candidate.ContentID)
	}
	if videoID == "" {
		return nil, "", fmt.Errorf("finalize short published_at: empty video id")
	}

	result := tx.WithContext(ctx).
		Model(&domain.YouTubeVideo{}).
		Where("video_id = ?", videoID).
		Update("published_at", publishedAt)
	if result.Error != nil {
		return nil, "", fmt.Errorf("update short published_at: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return nil, "", fmt.Errorf("update short published_at: video %s not found", videoID)
	}

	if err := txRepo.UpsertBatch(ctx, []*domain.YouTubeContentAlarmTracking{{
		Kind:              candidate.Kind,
		ContentID:         candidate.ContentID,
		ChannelID:         candidate.ChannelID,
		ActualPublishedAt: &publishedAt,
		DetectedAt:        candidate.DetectedAt,
	}}); err != nil {
		return nil, "", fmt.Errorf("upsert short tracking: %w", err)
	}
	if err := txRepo.UpsertSourcePostsBatch(ctx, []*domain.YouTubeCommunityShortsSourcePost{{
		Kind:              candidate.Kind,
		PostID:            candidate.PostID,
		ChannelID:         candidate.ChannelID,
		ActualPublishedAt: &publishedAt,
		DetectedAt:        candidate.DetectedAt,
	}}); err != nil {
		return nil, "", fmt.Errorf("upsert short source post: %w", err)
	}
	if err := txRepo.UpsertAlarmStateBatch(ctx, []*domain.YouTubeCommunityShortsAlarmState{{
		Kind:              candidate.Kind,
		PostID:            candidate.PostID,
		ContentID:         candidate.ContentID,
		ChannelID:         candidate.ChannelID,
		ActualPublishedAt: &publishedAt,
		DetectedAt:        candidate.DetectedAt,
	}}); err != nil {
		return nil, "", fmt.Errorf("upsert short alarm state: %w", err)
	}

	video, err := loadShortNotificationVideo(ctx, tx, videoID)
	if err != nil {
		return nil, "", fmt.Errorf("load short row for notification: %w", err)
	}

	if !shouldEnqueueRoutedNotification(routeDecider, domain.AlarmTypeShorts, candidate.ChannelID, publishedAt) {
		return nil, "route_decider_rejected", nil
	}

	authorizedAt := yttimestamp.Normalize(time.Now())
	claimed, err := txRepo.TryClaimAlarmState(ctx, &domain.YouTubeCommunityShortsAlarmState{
		Kind:              candidate.Kind,
		PostID:            candidate.PostID,
		ContentID:         candidate.ContentID,
		ChannelID:         candidate.ChannelID,
		ActualPublishedAt: &publishedAt,
		DetectedAt:        candidate.DetectedAt,
		AuthorizedAt:      &authorizedAt,
	})
	if err != nil {
		return nil, "", fmt.Errorf("claim short alarm state: %w", err)
	}
	if !claimed {
		return nil, "already_claimed", nil
	}

	return &domain.YouTubeNotificationOutbox{
		Kind:      candidate.Kind,
		ChannelID: candidate.ChannelID,
		ContentID: candidate.ContentID,
		Payload:   buildShortNotificationPayload(video, candidate.PostID),
		Status:    domain.OutboxStatusPending,
	}, "", nil
}

func (r *publishedAtResolverRepository) finalizeCommunity(
	ctx context.Context,
	tx *gorm.DB,
	txRepo *trackingrepo.GormRepository,
	candidate trackingrepo.PublishedAtResolutionCandidate,
	publishedAt time.Time,
	routeDecider NotificationRouteDecider,
) (*domain.YouTubeNotificationOutbox, string, error) {
	postID := normalizeContentID(candidate.Kind, candidate.PostID)
	if postID == "" {
		postID = normalizeContentID(candidate.Kind, candidate.ContentID)
	}
	if postID == "" {
		return nil, "", fmt.Errorf("finalize community published_at: empty post id")
	}

	result := tx.WithContext(ctx).
		Model(&domain.YouTubeCommunityPost{}).
		Where("post_id = ?", postID).
		Update("published_at", publishedAt)
	if result.Error != nil {
		return nil, "", fmt.Errorf("update community published_at: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return nil, "", fmt.Errorf("update community published_at: post %s not found", postID)
	}

	if err := txRepo.UpsertBatch(ctx, []*domain.YouTubeContentAlarmTracking{{
		Kind:              candidate.Kind,
		ContentID:         candidate.ContentID,
		ChannelID:         candidate.ChannelID,
		ActualPublishedAt: &publishedAt,
		DetectedAt:        candidate.DetectedAt,
	}}); err != nil {
		return nil, "", fmt.Errorf("upsert community tracking: %w", err)
	}
	if err := txRepo.UpsertSourcePostsBatch(ctx, []*domain.YouTubeCommunityShortsSourcePost{{
		Kind:              candidate.Kind,
		PostID:            candidate.PostID,
		ChannelID:         candidate.ChannelID,
		ActualPublishedAt: &publishedAt,
		DetectedAt:        candidate.DetectedAt,
	}}); err != nil {
		return nil, "", fmt.Errorf("upsert community source post: %w", err)
	}
	if err := txRepo.UpsertAlarmStateBatch(ctx, []*domain.YouTubeCommunityShortsAlarmState{{
		Kind:              candidate.Kind,
		PostID:            candidate.PostID,
		ContentID:         candidate.ContentID,
		ChannelID:         candidate.ChannelID,
		ActualPublishedAt: &publishedAt,
		DetectedAt:        candidate.DetectedAt,
	}}); err != nil {
		return nil, "", fmt.Errorf("upsert community alarm state: %w", err)
	}

	post, err := loadCommunityNotificationPost(ctx, tx, postID)
	if err != nil {
		return nil, "", fmt.Errorf("load community row for notification: %w", err)
	}

	if !shouldEnqueueRoutedNotification(routeDecider, domain.AlarmTypeCommunity, candidate.ChannelID, publishedAt) {
		return nil, "route_decider_rejected", nil
	}

	authorizedAt := yttimestamp.Normalize(time.Now())
	claimed, err := txRepo.TryClaimAlarmState(ctx, &domain.YouTubeCommunityShortsAlarmState{
		Kind:              candidate.Kind,
		PostID:            candidate.PostID,
		ContentID:         candidate.ContentID,
		ChannelID:         candidate.ChannelID,
		ActualPublishedAt: &publishedAt,
		DetectedAt:        candidate.DetectedAt,
		AuthorizedAt:      &authorizedAt,
	})
	if err != nil {
		return nil, "", fmt.Errorf("claim community alarm state: %w", err)
	}
	if !claimed {
		return nil, "already_claimed", nil
	}

	return &domain.YouTubeNotificationOutbox{
		Kind:      candidate.Kind,
		ChannelID: candidate.ChannelID,
		ContentID: candidate.ContentID,
		Payload:   buildCommunityNotificationPayload(post, candidate.PostID),
		Status:    domain.OutboxStatusPending,
	}, "", nil
}

type shortNotificationVideoRow struct {
	VideoID       string     `gorm:"column:video_id"`
	ChannelID     string     `gorm:"column:channel_id"`
	Title         string     `gorm:"column:title"`
	Thumbnail     string     `gorm:"column:thumbnail"`
	Duration      string     `gorm:"column:duration"`
	PublishedText string     `gorm:"column:published_text"`
	PublishedAt   *time.Time `gorm:"column:published_at"`
	IsShort       bool       `gorm:"column:is_short"`
	IsLiveReplay  bool       `gorm:"column:is_live_replay"`
	ViewCount     int64      `gorm:"column:view_count"`
	FirstSeenAt   time.Time  `gorm:"column:first_seen_at"`
	LastSeenAt    time.Time  `gorm:"column:last_seen_at"`
}

func loadShortNotificationVideo(ctx context.Context, tx *gorm.DB, videoID string) (*domain.YouTubeVideo, error) {
	var row shortNotificationVideoRow
	if err := tx.WithContext(ctx).
		Model(&domain.YouTubeVideo{}).
		Select("video_id, channel_id, title, thumbnail, duration, published_text, published_at, is_short, is_live_replay, view_count, first_seen_at, last_seen_at").
		Take(&row, "video_id = ?", videoID).Error; err != nil {
		return nil, err
	}

	thumbnail, err := parseThumbnailsJSON(row.Thumbnail)
	if err != nil {
		return nil, fmt.Errorf("parse short thumbnail: %w", err)
	}

	return &domain.YouTubeVideo{
		VideoID:       row.VideoID,
		ChannelID:     row.ChannelID,
		Title:         row.Title,
		Thumbnail:     thumbnail,
		Duration:      row.Duration,
		PublishedText: row.PublishedText,
		PublishedAt:   row.PublishedAt,
		IsShort:       row.IsShort,
		IsLiveReplay:  row.IsLiveReplay,
		ViewCount:     row.ViewCount,
		FirstSeenAt:   row.FirstSeenAt,
		LastSeenAt:    row.LastSeenAt,
	}, nil
}

type communityNotificationPostRow struct {
	PostID        string     `gorm:"column:post_id"`
	ChannelID     string     `gorm:"column:channel_id"`
	AuthorName    string     `gorm:"column:author_name"`
	AuthorPhoto   string     `gorm:"column:author_photo"`
	ContentText   string     `gorm:"column:content_text"`
	PublishedText string     `gorm:"column:published_text"`
	PublishedAt   *time.Time `gorm:"column:published_at"`
	LikeCount     int64      `gorm:"column:like_count"`
	CommentCount  int64      `gorm:"column:comment_count"`
	Images        string     `gorm:"column:images"`
	AttachedVideo string     `gorm:"column:attached_video"`
	FirstSeenAt   time.Time  `gorm:"column:first_seen_at"`
	LastSeenAt    time.Time  `gorm:"column:last_seen_at"`
}

func loadCommunityNotificationPost(ctx context.Context, tx *gorm.DB, postID string) (*domain.YouTubeCommunityPost, error) {
	var row communityNotificationPostRow
	if err := tx.WithContext(ctx).
		Model(&domain.YouTubeCommunityPost{}).
		Select("post_id, channel_id, author_name, author_photo, content_text, published_text, published_at, like_count, comment_count, images, attached_video, first_seen_at, last_seen_at").
		Take(&row, "post_id = ?", postID).Error; err != nil {
		return nil, err
	}

	authorPhoto, err := parseThumbnailsJSON(row.AuthorPhoto)
	if err != nil {
		return nil, fmt.Errorf("parse community author_photo: %w", err)
	}
	images, err := parseThumbnailsJSON(row.Images)
	if err != nil {
		return nil, fmt.Errorf("parse community images: %w", err)
	}

	return &domain.YouTubeCommunityPost{
		PostID:        row.PostID,
		ChannelID:     row.ChannelID,
		AuthorName:    row.AuthorName,
		AuthorPhoto:   authorPhoto,
		ContentText:   row.ContentText,
		PublishedText: row.PublishedText,
		PublishedAt:   row.PublishedAt,
		LikeCount:     row.LikeCount,
		CommentCount:  row.CommentCount,
		Images:        images,
		AttachedVideo: row.AttachedVideo,
		FirstSeenAt:   row.FirstSeenAt,
		LastSeenAt:    row.LastSeenAt,
	}, nil
}

func parseThumbnailsJSON(raw string) (domain.ThumbnailsJSON, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "null" {
		return nil, nil
	}

	var thumbnails domain.ThumbnailsJSON
	if err := json.Unmarshal([]byte(raw), &thumbnails); err != nil {
		return nil, err
	}
	return thumbnails, nil
}
