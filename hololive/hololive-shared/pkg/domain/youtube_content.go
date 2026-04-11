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

package domain

import (
	"database/sql/driver"
	"fmt"
	"strings"
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
	PublishedAt   *time.Time     `json:"published_at,omitempty"`
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
	PublishedAt   *time.Time     `json:"published_at,omitempty"`
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

// YouTubeContentAlarmTracking: 커뮤니티/쇼츠 알람 시각 추적
// 동일 콘텐츠에 대해 실제 게시 시각, 최초 감지 시각, 최초 성공 발송 시각과 저장된 지연 분류값을 보존한다.
type YouTubeContentAlarmTracking struct {
	Kind                        OutboxKind                        `gorm:"primaryKey;size:20;uniqueIndex:idx_ycat_kind_canonical_content,priority:1" json:"kind"`
	ContentID                   string                            `gorm:"primaryKey;size:50" json:"content_id"`
	CanonicalContentID          string                            `gorm:"size:50;not null;uniqueIndex:idx_ycat_kind_canonical_content,priority:2" json:"canonical_content_id"`
	ChannelID                   string                            `gorm:"size:50;not null;index:idx_ycat_channel_detected" json:"channel_id"`
	ActualPublishedAt           *time.Time                        `json:"actual_published_at,omitempty"`
	DetectedAt                  time.Time                         `gorm:"not null;index:idx_ycat_detected_at;index:idx_ycat_channel_detected" json:"detected_at"`
	AlarmSentAt                 *time.Time                        `gorm:"index:idx_ycat_alarm_sent_at" json:"alarm_sent_at,omitempty"`
	AlarmLatencyMillis          *int64                            `json:"alarm_latency_millis,omitempty"`
	AlarmLatencyExceeded        *bool                             `json:"alarm_latency_exceeded,omitempty"`
	DeliveryStatus              YouTubeContentAlarmDeliveryStatus `gorm:"size:20;not null;default:'PENDING';index:idx_ycat_delivery_status" json:"delivery_status"`
	LatencyClassificationStatus string                            `gorm:"size:40" json:"latency_classification_status,omitempty"`
	DelaySource                 string                            `gorm:"size:40" json:"delay_source,omitempty"`
	InternalDelayCause          string                            `gorm:"size:40" json:"internal_delay_cause,omitempty"`
	CreatedAt                   time.Time                         `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt                   time.Time                         `gorm:"autoUpdateTime" json:"updated_at"`
}

// TableName: GORM 테이블 이름
func (YouTubeContentAlarmTracking) TableName() string {
	return "youtube_content_alarm_tracking"
}

type YouTubeContentAlarmDeliveryStatus string

const (
	YouTubeContentAlarmDeliveryStatusPending YouTubeContentAlarmDeliveryStatus = "PENDING"
	YouTubeContentAlarmDeliveryStatusSent    YouTubeContentAlarmDeliveryStatus = "SENT"
)

func ResolveYouTubeContentAlarmDeliveryStatus(alarmSentAt *time.Time) YouTubeContentAlarmDeliveryStatus {
	if alarmSentAt != nil && !alarmSentAt.IsZero() {
		return YouTubeContentAlarmDeliveryStatusSent
	}
	return YouTubeContentAlarmDeliveryStatusPending
}

// YouTubeCommunityShortsSourcePost: community/shorts 검증용 원본 수집 목록
// 감지된 게시물의 채널 정보와 canonical post identifier를 관찰용 원본 집합으로 보존한다.
type YouTubeCommunityShortsSourcePost struct {
	Kind              OutboxKind `gorm:"primaryKey;size:20" json:"kind"`
	PostID            string     `gorm:"primaryKey;size:50" json:"post_id"`
	ChannelID         string     `gorm:"size:50;not null;index:idx_ycssp_channel_detected" json:"channel_id"`
	ActualPublishedAt *time.Time `json:"actual_published_at,omitempty"`
	DetectedAt        time.Time  `gorm:"not null;index:idx_ycssp_detected_at;index:idx_ycssp_channel_detected" json:"detected_at"`
	CreatedAt         time.Time  `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt         time.Time  `gorm:"autoUpdateTime" json:"updated_at"`
}

// TableName: GORM 테이블 이름
func (YouTubeCommunityShortsSourcePost) TableName() string {
	return "youtube_community_shorts_source_posts"
}

// YouTubeCommunityShortsAlarmState: community/shorts 게시물별 단일 알람 발송 상태
// canonical post identifier를 루트 키로 사용해 게시물당 하나의 상태 레코드만 유지한다.
type YouTubeCommunityShortsAlarmState struct {
	Kind              OutboxKind                             `gorm:"primaryKey;size:20;uniqueIndex:idx_ycsas_kind_content,priority:1" json:"kind"`
	PostID            string                                 `gorm:"primaryKey;size:50" json:"post_id"`
	ContentID         string                                 `gorm:"size:50;not null;uniqueIndex:idx_ycsas_kind_content,priority:2" json:"content_id"`
	ChannelID         string                                 `gorm:"size:50;not null;index:idx_ycsas_channel_detected" json:"channel_id"`
	ActualPublishedAt *time.Time                             `json:"actual_published_at,omitempty"`
	DetectedAt        time.Time                              `gorm:"not null;index:idx_ycsas_detected_at;index:idx_ycsas_channel_detected" json:"detected_at"`
	AuthorizedAt      *time.Time                             `gorm:"index:idx_ycsas_authorized_at" json:"authorized_at,omitempty"`
	AlarmSentAt       *time.Time                             `gorm:"index:idx_ycsas_alarm_sent_at" json:"alarm_sent_at,omitempty"`
	DeliveryStatus    YouTubeCommunityShortsAlarmStateStatus `gorm:"size:20;not null;default:'DETECTED';index:idx_ycsas_delivery_status" json:"delivery_status"`
	CreatedAt         time.Time                              `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt         time.Time                              `gorm:"autoUpdateTime" json:"updated_at"`
}

func (YouTubeCommunityShortsAlarmState) TableName() string {
	return "youtube_community_shorts_alarm_states"
}

type YouTubeCommunityShortsAlarmStateStatus string

const (
	YouTubeCommunityShortsAlarmStateStatusDetected YouTubeCommunityShortsAlarmStateStatus = "DETECTED"
	YouTubeCommunityShortsAlarmStateStatusEnqueued YouTubeCommunityShortsAlarmStateStatus = "ENQUEUED"
	YouTubeCommunityShortsAlarmStateStatusSent     YouTubeCommunityShortsAlarmStateStatus = "SENT"
)

func ResolveYouTubeCommunityShortsAlarmStateStatus(authorizedAt *time.Time, alarmSentAt *time.Time) YouTubeCommunityShortsAlarmStateStatus {
	if alarmSentAt != nil && !alarmSentAt.IsZero() {
		return YouTubeCommunityShortsAlarmStateStatusSent
	}
	if authorizedAt != nil && !authorizedAt.IsZero() {
		return YouTubeCommunityShortsAlarmStateStatusEnqueued
	}
	return YouTubeCommunityShortsAlarmStateStatusDetected
}

// YouTubeCommunityShortsObservationPostBaseline: 24시간 관찰 종료 시점에 고정한 community/shorts 게시물 기준 목록
// 동일 observation key에 대해 dedup 완료된 게시물 집합을 이후 검증에서 재사용한다.
type YouTubeCommunityShortsObservationPostBaseline struct {
	RuntimeName       string     `gorm:"primaryKey;size:50" json:"runtime_name"`
	BigBangCutoverAt  time.Time  `gorm:"column:bigbang_cutover_at;primaryKey" json:"bigbang_cutover_at"`
	Kind              OutboxKind `gorm:"primaryKey;size:20" json:"kind"`
	PostID            string     `gorm:"primaryKey;size:50" json:"post_id"`
	ChannelID         string     `gorm:"size:50;not null;index:idx_ycsopb_channel_detected" json:"channel_id"`
	ActualPublishedAt *time.Time `json:"actual_published_at,omitempty"`
	DetectedAt        time.Time  `gorm:"not null;index:idx_ycsopb_channel_detected" json:"detected_at"`
	FinalizedAt       time.Time  `gorm:"not null" json:"finalized_at"`
	CreatedAt         time.Time  `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt         time.Time  `gorm:"autoUpdateTime" json:"updated_at"`
}

// TableName: GORM 테이블 이름
func (YouTubeCommunityShortsObservationPostBaseline) TableName() string {
	return "youtube_community_shorts_observation_post_baselines"
}

// YouTubeCommunityShortsObservationWindow: 빅뱅 배포 직후 24시간 관찰 구간 메타데이터
// 동일 cutover/runtime 조합에 대해 최초 감지된 배포 완료 시각과 관찰 창을 보존한다.
type YouTubeCommunityShortsObservationWindow struct {
	RuntimeName             string     `gorm:"primaryKey;size:50" json:"runtime_name"`
	BigBangCutoverAt        time.Time  `gorm:"column:bigbang_cutover_at;primaryKey" json:"bigbang_cutover_at"`
	AppVersion              string     `gorm:"size:100;not null" json:"app_version"`
	TargetChannelCount      int        `gorm:"not null" json:"target_channel_count"`
	DeploymentCompletedAt   time.Time  `gorm:"not null;index:idx_ycsow_deploy_completed" json:"deployment_completed_at"`
	ObservationStartedAt    time.Time  `gorm:"not null;index:idx_ycsow_window_start" json:"observation_started_at"`
	ObservationEndedAt      time.Time  `gorm:"not null;index:idx_ycsow_window_end" json:"observation_ended_at"`
	ClosedAt                *time.Time `gorm:"column:closed_at;index:idx_ycsow_closed_at" json:"closed_at,omitempty"`
	FinalizedPostBaselineAt *time.Time `gorm:"column:finalized_post_baseline_at;index:idx_ycsow_finalized_post_baseline_at" json:"finalized_post_baseline_at,omitempty"`
	FinalizedPostCount      int        `gorm:"not null;default:0" json:"finalized_post_count"`
	CreatedAt               time.Time  `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt               time.Time  `gorm:"autoUpdateTime" json:"updated_at"`
}

// TableName: GORM 테이블 이름
func (YouTubeCommunityShortsObservationWindow) TableName() string {
	return "youtube_community_shorts_observation_windows"
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

const youtubeNotificationDedupeKeyPrefix = "youtube-notification"

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

// BuildYouTubeNotificationDedupeKey는 outbox kind/content_id에서 dedupe key를 생성한다.
func BuildYouTubeNotificationDedupeKey(kind OutboxKind, contentID string) (string, error) {
	normalizedKind := strings.TrimSpace(string(kind))
	if normalizedKind == "" {
		return "", fmt.Errorf("build youtube notification dedupe key: kind is empty")
	}

	normalizedContentID := strings.TrimSpace(contentID)
	if normalizedContentID == "" {
		return "", fmt.Errorf("build youtube notification dedupe key: content id is empty")
	}

	return fmt.Sprintf("%s:%s:%s", youtubeNotificationDedupeKeyPrefix, normalizedKind, normalizedContentID), nil
}

// DedupeKey는 outbox row의 dedupe key를 반환한다.
func (o YouTubeNotificationOutbox) DedupeKey() (string, error) {
	return BuildYouTubeNotificationDedupeKey(o.Kind, o.ContentID)
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
	&YouTubeContentAlarmTracking{},
	&YouTubeCommunityShortsSourcePost{},
	&YouTubeCommunityShortsAlarmState{},
	&YouTubeCommunityShortsObservationWindow{},
	&YouTubeNotificationDeliveryTelemetry{},
	&YouTubeNotificationDelivery{},
	&YouTubeLiveSession{},
	&YouTubeLiveViewerSample{},
	&YouTubeStreamStats{},
}
