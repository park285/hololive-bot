// Package youtubeoutbox는 YouTube 알림 outbox payload의 JSON 계약을 정의한다.
//
// producer(API의 canonical 저장과 커뮤니티 처리)는 domain 값을 이 타입으로 필드마다 옮겨 저장하고,
// alarm-worker의 renderer·묶음 처리와 저장 직전 검증은 같은 타입으로 읽는다. domain 구조체의 필드나
// JSON 태그가 바뀌어도 저장 payload가 함께 바뀌지 않게 하는 경계다. 필드 이름·순서·omitempty는
// 이미 저장된 payload와 같아야 하며, 바꾸려면 저장된 행을 읽는 모든 소비자를 함께 바꿔야 한다.
package youtubeoutbox

import (
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

// VideoFields는 영상 계열 payload(새 영상·라이브·쇼츠)가 공유하는 영상 필드다.
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

// Video는 새 영상과 라이브 알림의 payload다. 예정 시각과 프리미어 여부는 알려진 경우에만 싣는다.
type Video struct {
	VideoFields

	ScheduledStartAt *time.Time `json:"scheduled_start_at,omitempty"`
	IsPremiere       *bool      `json:"is_premiere,omitempty"`
}

// Short는 쇼츠 알림의 payload다. CanonicalPostID는 `short:` 접두사를 가진 정규 content ID이며 항상 싣는다.
type Short struct {
	VideoFields

	CanonicalPostID string `json:"canonical_post_id"`
}

// Community는 커뮤니티 게시글 알림의 payload다. CanonicalPostID는 `community:` 접두사를 가진 정규
// content ID이며 항상 싣는다.
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

// NewVideoFields는 domain 영상 값을 payload 필드로 옮긴다. 새 domain 필드는 여기에 명시적으로 추가해야 payload에 실린다.
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

// NewCommunity는 domain 게시글 값을 payload로 옮긴다. 새 domain 필드는 여기에 명시적으로 추가해야 payload에 실린다.
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
