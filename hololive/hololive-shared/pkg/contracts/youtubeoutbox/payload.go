package youtubeoutbox

import (
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

type VideoFields struct {
	VideoID       string                `json:"video_id"`
	ChannelID     string                `json:"channel_id"`
	Title         string                `json:"title"`
	Thumbnail     domain.ThumbnailsJSON `json:"thumbnail,omitempty"`
	Duration      string                `json:"duration,omitempty"`
	PublishedText string                `json:"published_text,omitempty"`
	PublishedAt   *time.Time            `json:"published_at,omitempty"`
	IsShort       bool                  `json:"is_short"`
	IsLiveReplay  bool                  `json:"is_live_replay"`
	ViewCount     int64                 `json:"view_count"`
	FirstSeenAt   time.Time             `json:"first_seen_at"`
	LastSeenAt    time.Time             `json:"last_seen_at"`
}

type Video struct {
	VideoFields

	ScheduledStartAt *time.Time `json:"scheduled_start_at,omitempty"`
	IsPremiere       *bool      `json:"is_premiere,omitempty"`
}

type Short struct {
	VideoFields

	CanonicalPostID string `json:"canonical_post_id"`
}

type Community struct {
	PostID        string                `json:"post_id"`
	ChannelID     string                `json:"channel_id"`
	AuthorName    string                `json:"author_name,omitempty"`
	AuthorPhoto   domain.ThumbnailsJSON `json:"author_photo,omitempty"`
	ContentText   string                `json:"content_text,omitempty"`
	PublishedText string                `json:"published_text,omitempty"`
	PublishedAt   *time.Time            `json:"published_at,omitempty"`
	LikeCount     int64                 `json:"like_count"`
	CommentCount  int64                 `json:"comment_count"`
	Images        domain.ThumbnailsJSON `json:"images,omitempty"`
	AttachedVideo string                `json:"attached_video,omitempty"`
	FirstSeenAt   time.Time             `json:"first_seen_at"`
	LastSeenAt    time.Time             `json:"last_seen_at"`

	CanonicalPostID string `json:"canonical_post_id"`
}

func NewVideoFields(video *domain.YouTubeVideo) VideoFields {
	return VideoFields{
		VideoID:       video.VideoID,
		ChannelID:     video.ChannelID,
		Title:         video.Title,
		Thumbnail:     video.Thumbnail,
		Duration:      video.Duration,
		PublishedText: video.PublishedText,
		PublishedAt:   video.PublishedAt,
		IsShort:       video.IsShort,
		IsLiveReplay:  video.IsLiveReplay,
		ViewCount:     video.ViewCount,
		FirstSeenAt:   video.FirstSeenAt,
		LastSeenAt:    video.LastSeenAt,
	}
}

func NewCommunity(post *domain.YouTubeCommunityPost, canonicalPostID string) Community {
	return Community{
		PostID:          post.PostID,
		ChannelID:       post.ChannelID,
		AuthorName:      post.AuthorName,
		AuthorPhoto:     post.AuthorPhoto,
		ContentText:     post.ContentText,
		PublishedText:   post.PublishedText,
		PublishedAt:     post.PublishedAt,
		LikeCount:       post.LikeCount,
		CommentCount:    post.CommentCount,
		Images:          post.Images,
		AttachedVideo:   post.AttachedVideo,
		FirstSeenAt:     post.FirstSeenAt,
		LastSeenAt:      post.LastSeenAt,
		CanonicalPostID: canonicalPostID,
	}
}
