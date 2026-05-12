package poller

import (
	"context"
	"fmt"
	"strings"
	"time"

	json "github.com/park285/llm-kakao-bots/shared-go/pkg/json"
	"gorm.io/gorm"

	"github.com/kapu/hololive-shared/pkg/domain"
	trackingrepo "github.com/kapu/hololive-shared/pkg/service/youtube/tracking"
)

func newShortPublishedAtNotification(
	candidate trackingrepo.PublishedAtResolutionCandidate,
	video *domain.YouTubeVideo,
) *domain.YouTubeNotificationOutbox {
	return &domain.YouTubeNotificationOutbox{
		Kind:      candidate.Kind,
		ChannelID: candidate.ChannelID,
		ContentID: candidate.ContentID,
		Payload:   buildShortNotificationPayload(video, candidate.PostID),
		Status:    domain.OutboxStatusPending,
	}
}

func newCommunityPublishedAtNotification(
	candidate trackingrepo.PublishedAtResolutionCandidate,
	post *domain.YouTubeCommunityPost,
) *domain.YouTubeNotificationOutbox {
	return &domain.YouTubeNotificationOutbox{
		Kind:      candidate.Kind,
		ChannelID: candidate.ChannelID,
		ContentID: candidate.ContentID,
		Payload:   buildCommunityNotificationPayload(post, candidate.PostID),
		Status:    domain.OutboxStatusPending,
	}
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
		return nil, fmt.Errorf("parse thumbnails json: %w", err)
	}
	return thumbnails, nil
}
