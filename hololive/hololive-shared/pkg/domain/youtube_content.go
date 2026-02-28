package domain

import (
	"database/sql/driver"
	"fmt"
	"time"

	"github.com/park285/llm-kakao-bots/shared-go/pkg/json"
)

// YouTubeChannelStatsSnapshot: 채널 통계 스냅샷 (구독자 그래프의 원천)
type YouTubeChannelStatsSnapshot struct {
	ChannelID       string    `gorm:"primaryKey;size:50" json:"channel_id"`
	CapturedAt      time.Time `gorm:"primaryKey" json:"captured_at"`
	SubscriberCount int64     `json:"subscriber_count"`
	ViewCount       int64     `json:"view_count"`
	VideoCount      int64     `json:"video_count"`
	JoinedDate      int64     `json:"joined_date,omitempty"` // Unix timestamp
	Description     string    `gorm:"type:text" json:"description,omitempty"`
	Country         string    `gorm:"size:50" json:"country,omitempty"`
	Handle          string    `gorm:"size:100" json:"handle,omitempty"`
}

// TableName: GORM 테이블 이름
func (YouTubeChannelStatsSnapshot) TableName() string {
	return "youtube_channel_stats_snapshots"
}

// YouTubeChannelProfile: 채널 프로필 (아바타/배너)
type YouTubeChannelProfile struct {
	ChannelID string         `gorm:"primaryKey;size:50" json:"channel_id"`
	Avatar    ThumbnailsJSON `gorm:"type:jsonb" json:"avatar,omitempty"`
	Banner    ThumbnailsJSON `gorm:"type:jsonb" json:"banner,omitempty"`
	UpdatedAt time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
}

// TableName: GORM 테이블 이름
func (YouTubeChannelProfile) TableName() string {
	return "youtube_channel_profiles"
}

// YouTubeVideo: 업로드된 영상 (일반 영상/쇼츠)
type YouTubeVideo struct {
	VideoID       string         `gorm:"primaryKey;size:20" json:"video_id"`
	ChannelID     string         `gorm:"size:50;index:idx_yv_channel_first_seen" json:"channel_id"`
	Title         string         `gorm:"size:500" json:"title"`
	Thumbnail     ThumbnailsJSON `gorm:"type:jsonb" json:"thumbnail,omitempty"`
	Duration      string         `gorm:"size:20" json:"duration,omitempty"`
	PublishedText string         `gorm:"size:100" json:"published_text,omitempty"`
	IsShort       bool           `gorm:"default:false;index:idx_yv_channel_is_short" json:"is_short"`
	IsLiveReplay  bool           `gorm:"default:false" json:"is_live_replay"`
	ViewCount     int64          `json:"view_count,omitempty"`
	FirstSeenAt   time.Time      `gorm:"autoCreateTime;index:idx_yv_channel_first_seen" json:"first_seen_at"`
	LastSeenAt    time.Time      `gorm:"autoUpdateTime" json:"last_seen_at"`
}

// TableName: GORM 테이블 이름
func (YouTubeVideo) TableName() string {
	return "youtube_videos"
}

// YouTubeCommunityPost: 커뮤니티 포스트
type YouTubeCommunityPost struct {
	PostID        string         `gorm:"primaryKey;size:50" json:"post_id"`
	ChannelID     string         `gorm:"size:50;index:idx_ycp_channel_first_seen" json:"channel_id"`
	AuthorName    string         `gorm:"size:200" json:"author_name,omitempty"`
	AuthorPhoto   ThumbnailsJSON `gorm:"type:jsonb" json:"author_photo,omitempty"`
	ContentText   string         `gorm:"type:text" json:"content_text,omitempty"`
	PublishedText string         `gorm:"size:100" json:"published_text,omitempty"`
	LikeCount     int64          `json:"like_count,omitempty"`
	CommentCount  int64          `json:"comment_count,omitempty"`
	Images        ThumbnailsJSON `gorm:"type:jsonb" json:"images,omitempty"`
	AttachedVideo string         `gorm:"size:20" json:"attached_video,omitempty"`
	FirstSeenAt   time.Time      `gorm:"autoCreateTime;index:idx_ycp_channel_first_seen" json:"first_seen_at"`
	LastSeenAt    time.Time      `gorm:"autoUpdateTime" json:"last_seen_at"`
}

// TableName: GORM 테이블 이름
func (YouTubeCommunityPost) TableName() string {
	return "youtube_community_posts"
}

// WatermarkType: 워터마크 타입
type WatermarkType string

const (
	WatermarkTypeVideo         WatermarkType = "VIDEO"
	WatermarkTypeShort         WatermarkType = "SHORT"
	WatermarkTypeCommunityPost WatermarkType = "COMMUNITY_POST"
)

// YouTubeContentWatermark: 콘텐츠 워터마크 (초기 동기화 및 중복 알림 방지)
type YouTubeContentWatermark struct {
	ChannelID     string        `gorm:"primaryKey;size:50" json:"channel_id"`
	WatermarkType WatermarkType `gorm:"primaryKey;size:20" json:"watermark_type"`
	Initialized   bool          `gorm:"default:false" json:"initialized"`
	LastContentID string        `gorm:"size:50" json:"last_content_id,omitempty"`
	UpdatedAt     time.Time     `gorm:"autoUpdateTime" json:"updated_at"`
}

// TableName: GORM 테이블 이름
func (YouTubeContentWatermark) TableName() string {
	return "youtube_content_watermarks"
}

// OutboxKind: 알림 종류
type OutboxKind string

const (
	OutboxKindNewVideo      OutboxKind = "NEW_VIDEO"
	OutboxKindNewShort      OutboxKind = "NEW_SHORT"
	OutboxKindCommunityPost OutboxKind = "COMMUNITY_POST"
	OutboxKindMilestone     OutboxKind = "MILESTONE"
)

func (k OutboxKind) ToAlarmType() AlarmType {
	switch k {
	case OutboxKindNewShort:
		return AlarmTypeShorts
	case OutboxKindCommunityPost:
		return AlarmTypeCommunity
	default:
		return AlarmTypeLive
	}
}

func (k OutboxKind) ToTemplateKey() TemplateKey {
	switch k {
	case OutboxKindNewVideo:
		return TemplateKeyOutboxVideo
	case OutboxKindNewShort:
		return TemplateKeyOutboxShorts
	case OutboxKindCommunityPost:
		return TemplateKeyOutboxCommunity
	case OutboxKindMilestone:
		return TemplateKeyOutboxMilestone
	default:
		return TemplateKeyOutboxVideo
	}
}

// OutboxStatus: 알림 상태
type OutboxStatus string

const (
	OutboxStatusPending OutboxStatus = "PENDING"
	OutboxStatusSent    OutboxStatus = "SENT"
	OutboxStatusFailed  OutboxStatus = "FAILED"
)

// YouTubeNotificationOutbox: 알림 Outbox (전송/재시도/중복방지)
type YouTubeNotificationOutbox struct {
	ID            int64        `gorm:"primaryKey;autoIncrement" json:"id"`
	Kind          OutboxKind   `gorm:"size:20;not null" json:"kind"`
	ChannelID     string       `gorm:"size:50;not null" json:"channel_id"`
	ContentID     string       `gorm:"size:50;not null;uniqueIndex:idx_yno_kind_content" json:"content_id"`
	Payload       string       `gorm:"type:jsonb;not null" json:"payload"`
	Status        OutboxStatus `gorm:"size:20;not null;default:'PENDING';index:idx_yno_status_created" json:"status"`
	AttemptCount  int          `gorm:"not null;default:0" json:"attempt_count"`
	NextAttemptAt time.Time    `gorm:"not null;default:NOW()" json:"next_attempt_at"`
	CreatedAt     time.Time    `gorm:"autoCreateTime;index:idx_yno_status_created" json:"created_at"`
	LockedAt      *time.Time   `json:"locked_at,omitempty"`
	SentAt        *time.Time   `json:"sent_at,omitempty"`
	Error         string       `gorm:"type:text" json:"error,omitempty"`
}

// TableName: GORM 테이블 이름
func (YouTubeNotificationOutbox) TableName() string {
	return "youtube_notification_outbox"
}

// LiveStatus: 라이브 상태
type LiveStatus string

const (
	LiveStatusUpcoming LiveStatus = "UPCOMING"
	LiveStatusLive     LiveStatus = "LIVE"
	LiveStatusEnded    LiveStatus = "ENDED"
)

// YouTubeLiveSession: 라이브 세션 (UPCOMING/LIVE/ENDED)
type YouTubeLiveSession struct {
	VideoID            string     `gorm:"primaryKey;size:20" json:"video_id"`
	ChannelID          string     `gorm:"size:50;index:idx_yls_channel_last_seen" json:"channel_id"`
	Status             LiveStatus `gorm:"size:20;not null;index:idx_yls_status_last_seen" json:"status"`
	Title              string     `gorm:"size:500" json:"title,omitempty"`
	ScheduledStartTime *time.Time `json:"scheduled_start_time,omitempty"`
	StartedAt          *time.Time `json:"started_at,omitempty"`
	EndedAt            *time.Time `json:"ended_at,omitempty"`
	LastSeenAt         time.Time  `gorm:"autoUpdateTime;index:idx_yls_status_last_seen,idx_yls_channel_last_seen" json:"last_seen_at"`
}

// TableName: GORM 테이블 이름
func (YouTubeLiveSession) TableName() string {
	return "youtube_live_sessions"
}

// YouTubeLiveViewerSample: 라이브 시청자 샘플 (시계열 데이터)
type YouTubeLiveViewerSample struct {
	VideoID           string    `gorm:"primaryKey;size:20" json:"video_id"`
	CapturedAt        time.Time `gorm:"primaryKey" json:"captured_at"`
	ChannelID         string    `gorm:"size:50;index:idx_ylvs_channel_time" json:"channel_id"`
	ConcurrentViewers int       `json:"concurrent_viewers"`
}

// TableName: GORM 테이블 이름
func (YouTubeLiveViewerSample) TableName() string {
	return "youtube_live_viewer_samples"
}

// YouTubeStreamStats: 방송 집계 통계 (평균/최대 시청자)
type YouTubeStreamStats struct {
	VideoID              string     `gorm:"primaryKey;size:20" json:"video_id"`
	ChannelID            string     `gorm:"size:50;index:idx_yss_channel_ended" json:"channel_id"`
	StartedAt            *time.Time `json:"started_at,omitempty"`
	EndedAt              *time.Time `gorm:"index:idx_yss_channel_ended" json:"ended_at,omitempty"`
	MaxConcurrentViewers int        `json:"max_concurrent_viewers,omitempty"`
	AvgConcurrentViewers int        `json:"avg_concurrent_viewers,omitempty"`
	SampleCount          int        `gorm:"default:0" json:"sample_count"`
	UpdatedAt            time.Time  `gorm:"autoUpdateTime" json:"updated_at"`
}

// TableName: GORM 테이블 이름
func (YouTubeStreamStats) TableName() string {
	return "youtube_stream_stats"
}

// ThumbnailsJSON: JSON으로 저장되는 썸네일 배열
type ThumbnailsJSON []ThumbnailEntry

// ThumbnailEntry: 썸네일 엔트리
type ThumbnailEntry struct {
	URL    string `json:"url"`
	Width  int    `json:"width,omitempty"`
	Height int    `json:"height,omitempty"`
}

// Value: driver.Valuer 구현 (DB 저장 시)
func (t ThumbnailsJSON) Value() (driver.Value, error) {
	if t == nil {
		return nil, nil
	}
	data, err := json.Marshal(t)
	if err != nil {
		return nil, fmt.Errorf("marshal thumbnails: %w", err)
	}
	// pgx stdlib 드라이버는 []byte를 bytea로 해석하므로, jsonb 컬럼에는 string으로 반환해야 한다.
	return string(data), nil
}

// Scan: sql.Scanner 구현 (DB 로드 시)
func (t *ThumbnailsJSON) Scan(value any) error {
	if value == nil {
		*t = nil
		return nil
	}
	bytes, ok := value.([]byte)
	if !ok {
		return fmt.Errorf("failed to scan ThumbnailsJSON: expected []byte, got %T", value)
	}
	if err := json.Unmarshal(bytes, t); err != nil {
		return fmt.Errorf("unmarshal thumbnails: %w", err)
	}
	return nil
}

// YouTubeModels: GORM AutoMigrate에 사용할 모델 목록
var YouTubeModels = []any{
	&YouTubeChannelStatsSnapshot{},
	&YouTubeChannelProfile{},
	&YouTubeVideo{},
	&YouTubeCommunityPost{},
	&YouTubeContentWatermark{},
	&YouTubeNotificationOutbox{},
	&YouTubeLiveSession{},
	&YouTubeLiveViewerSample{},
	&YouTubeStreamStats{},
}
