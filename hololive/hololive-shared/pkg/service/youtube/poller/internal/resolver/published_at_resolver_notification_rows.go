package resolver

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/georgysavva/scany/v2/pgxscan"
	"github.com/kapu/hololive-shared/internal/dbx"
	"github.com/kapu/hololive-shared/pkg/domain"
	json "github.com/park285/shared-go/pkg/json"
)

type shortNotificationVideoRow struct {
	VideoID       string     `db:"video_id"`
	ChannelID     string     `db:"channel_id"`
	Title         string     `db:"title"`
	Thumbnail     string     `db:"thumbnail"`
	Duration      string     `db:"duration"`
	PublishedText string     `db:"published_text"`
	PublishedAt   *time.Time `db:"published_at"`
	IsShort       bool       `db:"is_short"`
	IsLiveReplay  bool       `db:"is_live_replay"`
	ViewCount     int64      `db:"view_count"`
	FirstSeenAt   time.Time  `db:"first_seen_at"`
	LastSeenAt    time.Time  `db:"last_seen_at"`
}

func loadShortNotificationVideo(ctx context.Context, tx dbx.Querier, videoID string) (*domain.YouTubeVideo, error) {
	var row shortNotificationVideoRow
	if err := pgxscan.Get(ctx, tx, &row, `
		SELECT video_id,
			channel_id,
			title,
			COALESCE(thumbnail::text, '') AS thumbnail,
			duration,
			published_text,
			published_at,
			is_short,
			is_live_replay,
			view_count,
			first_seen_at,
			last_seen_at
		FROM youtube_videos
		WHERE video_id = $1`,
		videoID,
	); err != nil {
		return nil, err
	}

	thumbnail, err := parseThumbnailsJSON(row.Thumbnail)
	if err != nil {
		return nil, fmt.Errorf("parse short thumbnail: %w", err)
	}
	publishedAt := utcTimePtr(row.PublishedAt)

	return &domain.YouTubeVideo{
		VideoID:       row.VideoID,
		ChannelID:     row.ChannelID,
		Title:         row.Title,
		Thumbnail:     thumbnail,
		Duration:      row.Duration,
		PublishedText: row.PublishedText,
		PublishedAt:   publishedAt,
		IsShort:       row.IsShort,
		IsLiveReplay:  row.IsLiveReplay,
		ViewCount:     row.ViewCount,
		FirstSeenAt:   row.FirstSeenAt.UTC(),
		LastSeenAt:    row.LastSeenAt.UTC(),
	}, nil
}

type communityNotificationPostRow struct {
	PostID        string     `db:"post_id"`
	ChannelID     string     `db:"channel_id"`
	AuthorName    string     `db:"author_name"`
	AuthorPhoto   string     `db:"author_photo"`
	ContentText   string     `db:"content_text"`
	PublishedText string     `db:"published_text"`
	PublishedAt   *time.Time `db:"published_at"`
	LikeCount     int64      `db:"like_count"`
	CommentCount  int64      `db:"comment_count"`
	Images        string     `db:"images"`
	AttachedVideo string     `db:"attached_video"`
	FirstSeenAt   time.Time  `db:"first_seen_at"`
	LastSeenAt    time.Time  `db:"last_seen_at"`
}

func loadCommunityNotificationPost(ctx context.Context, tx dbx.Querier, postID string) (*domain.YouTubeCommunityPost, error) {
	var row communityNotificationPostRow
	if err := pgxscan.Get(ctx, tx, &row, `
		SELECT post_id,
			channel_id,
			author_name,
			COALESCE(author_photo::text, '') AS author_photo,
			content_text,
			published_text,
			published_at,
			like_count,
			comment_count,
			COALESCE(images::text, '') AS images,
			attached_video,
			first_seen_at,
			last_seen_at
		FROM youtube_community_posts
		WHERE post_id = $1`,
		postID,
	); err != nil {
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
	publishedAt := utcTimePtr(row.PublishedAt)

	return &domain.YouTubeCommunityPost{
		PostID:        row.PostID,
		ChannelID:     row.ChannelID,
		AuthorName:    row.AuthorName,
		AuthorPhoto:   authorPhoto,
		ContentText:   row.ContentText,
		PublishedText: row.PublishedText,
		PublishedAt:   publishedAt,
		LikeCount:     row.LikeCount,
		CommentCount:  row.CommentCount,
		Images:        images,
		AttachedVideo: row.AttachedVideo,
		FirstSeenAt:   row.FirstSeenAt.UTC(),
		LastSeenAt:    row.LastSeenAt.UTC(),
	}, nil
}

func utcTimePtr(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	normalized := value.UTC()
	return &normalized
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
