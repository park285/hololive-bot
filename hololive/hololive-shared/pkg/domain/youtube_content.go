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
	"fmt"
	"strings"
	"time"
)

type YouTubeChannelStatsSnapshot struct {
	ChannelID       string    `db:"channel_id" json:"channel_id"`
	CapturedAt      time.Time `db:"captured_at" json:"captured_at"`
	SubscriberCount int64     `json:"subscriber_count"`
	ViewCount       int64     `json:"view_count"`
	VideoCount      int64     `json:"video_count"`
	JoinedDate      int64     `json:"joined_date,omitempty"` // Unix timestamp
	Description     string    `db:"description" json:"description,omitempty"`
	Country         string    `db:"country" json:"country,omitempty"`
	Handle          string    `db:"handle" json:"handle,omitempty"`
}

func (YouTubeChannelStatsSnapshot) TableName() string {
	return "youtube_channel_stats_snapshots"
}

type YouTubeChannelProfile struct {
	ChannelID string         `db:"channel_id" json:"channel_id"`
	Avatar    ThumbnailsJSON `db:"avatar" json:"avatar,omitempty"`
	Banner    ThumbnailsJSON `db:"banner" json:"banner,omitempty"`
	UpdatedAt time.Time      `db:"updated_at" json:"updated_at"`
}

func (YouTubeChannelProfile) TableName() string {
	return "youtube_channel_profiles"
}

type YouTubeVideo struct {
	VideoID       string         `db:"video_id" json:"video_id"`
	ChannelID     string         `db:"channel_id" json:"channel_id"`
	Title         string         `db:"title" json:"title"`
	Thumbnail     ThumbnailsJSON `db:"thumbnail" json:"thumbnail,omitempty"`
	Duration      string         `db:"duration" json:"duration,omitempty"`
	PublishedText string         `db:"published_text" json:"published_text,omitempty"`
	PublishedAt   *time.Time     `json:"published_at,omitempty"`
	IsShort       bool           `db:"is_short" json:"is_short"`
	IsLiveReplay  bool           `db:"is_live_replay" json:"is_live_replay"`
	ViewCount     int64          `json:"view_count,omitempty"`
	FirstSeenAt   time.Time      `db:"first_seen_at" json:"first_seen_at"`
	LastSeenAt    time.Time      `db:"last_seen_at" json:"last_seen_at"`
}

func (YouTubeVideo) TableName() string {
	return "youtube_videos"
}

type YouTubeCommunityPost struct {
	PostID        string         `db:"post_id" json:"post_id"`
	ChannelID     string         `db:"channel_id" json:"channel_id"`
	AuthorName    string         `db:"author_name" json:"author_name,omitempty"`
	AuthorPhoto   ThumbnailsJSON `db:"author_photo" json:"author_photo,omitempty"`
	ContentText   string         `db:"content_text" json:"content_text,omitempty"`
	PublishedText string         `db:"published_text" json:"published_text,omitempty"`
	PublishedAt   *time.Time     `json:"published_at,omitempty"`
	LikeCount     int64          `json:"like_count,omitempty"`
	CommentCount  int64          `json:"comment_count,omitempty"`
	Images        ThumbnailsJSON `db:"images" json:"images,omitempty"`
	AttachedVideo string         `db:"attached_video" json:"attached_video,omitempty"`
	FirstSeenAt   time.Time      `db:"first_seen_at" json:"first_seen_at"`
	LastSeenAt    time.Time      `db:"last_seen_at" json:"last_seen_at"`
}

func (YouTubeCommunityPost) TableName() string {
	return "youtube_community_posts"
}

// 동일 콘텐츠에 대해 실제 게시 시각, 최초 감지 시각, 최초 성공 발송 시각과 저장된 지연 분류값을 보존한다.
type YouTubeContentAlarmTracking struct {
	Kind                        OutboxKind                        `db:"kind" json:"kind"`
	ContentID                   string                            `db:"content_id" json:"content_id"`
	CanonicalContentID          string                            `db:"canonical_content_id" json:"canonical_content_id"`
	ChannelID                   string                            `db:"channel_id" json:"channel_id"`
	ActualPublishedAt           *time.Time                        `json:"actual_published_at,omitempty"`
	DetectedAt                  time.Time                         `db:"detected_at" json:"detected_at"`
	AlarmSentAt                 *time.Time                        `db:"alarm_sent_at" json:"alarm_sent_at,omitempty"`
	AlarmLatencyMillis          *int64                            `json:"alarm_latency_millis,omitempty"`
	AlarmLatencyExceeded        *bool                             `json:"alarm_latency_exceeded,omitempty"`
	DeliveryStatus              YouTubeContentAlarmDeliveryStatus `db:"delivery_status" json:"delivery_status"`
	LatencyClassificationStatus string                            `db:"latency_classification_status" json:"latency_classification_status,omitempty"`
	DelaySource                 string                            `db:"delay_source" json:"delay_source,omitempty"`
	InternalDelayCause          string                            `db:"internal_delay_cause" json:"internal_delay_cause,omitempty"`
	CreatedAt                   time.Time                         `db:"created_at" json:"created_at"`
	UpdatedAt                   time.Time                         `db:"updated_at" json:"updated_at"`
}

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

// 감지된 게시물의 채널 정보와 canonical post identifier를 관찰용 원본 집합으로 보존한다.
type YouTubeCommunityShortsSourcePost struct {
	Kind              OutboxKind `db:"kind" json:"kind"`
	PostID            string     `db:"post_id" json:"post_id"`
	ChannelID         string     `db:"channel_id" json:"channel_id"`
	ActualPublishedAt *time.Time `json:"actual_published_at,omitempty"`
	DetectedAt        time.Time  `db:"detected_at" json:"detected_at"`
	CreatedAt         time.Time  `db:"created_at" json:"created_at"`
	UpdatedAt         time.Time  `db:"updated_at" json:"updated_at"`
}

func (YouTubeCommunityShortsSourcePost) TableName() string {
	return "youtube_community_shorts_source_posts"
}

// canonical post identifier를 루트 키로 사용해 게시물당 하나의 상태 레코드만 유지한다.
type YouTubeCommunityShortsAlarmState struct {
	Kind              OutboxKind                             `db:"kind" json:"kind"`
	PostID            string                                 `db:"post_id" json:"post_id"`
	ContentID         string                                 `db:"content_id" json:"content_id"`
	ChannelID         string                                 `db:"channel_id" json:"channel_id"`
	ActualPublishedAt *time.Time                             `json:"actual_published_at,omitempty"`
	DetectedAt        time.Time                              `db:"detected_at" json:"detected_at"`
	AuthorizedAt      *time.Time                             `db:"authorized_at" json:"authorized_at,omitempty"`
	AlarmSentAt       *time.Time                             `db:"alarm_sent_at" json:"alarm_sent_at,omitempty"`
	DeliveryStatus    YouTubeCommunityShortsAlarmStateStatus `db:"delivery_status" json:"delivery_status"`
	CreatedAt         time.Time                              `db:"created_at" json:"created_at"`
	UpdatedAt         time.Time                              `db:"updated_at" json:"updated_at"`
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

func ResolveYouTubeCommunityShortsAlarmStateStatus(authorizedAt, alarmSentAt *time.Time) YouTubeCommunityShortsAlarmStateStatus {
	if alarmSentAt != nil && !alarmSentAt.IsZero() {
		return YouTubeCommunityShortsAlarmStateStatusSent
	}
	if authorizedAt != nil && !authorizedAt.IsZero() {
		return YouTubeCommunityShortsAlarmStateStatusEnqueued
	}
	return YouTubeCommunityShortsAlarmStateStatusDetected
}

// 동일 cutover/runtime 조합에 대해 최초 감지된 배포 완료 시각과 관찰 창을 보존한다.
type YouTubeCommunityShortsObservationWindow struct {
	RuntimeName             string     `db:"runtime_name" json:"runtime_name"`
	BigBangCutoverAt        time.Time  `db:"bigbang_cutover_at" json:"bigbang_cutover_at"`
	AppVersion              string     `db:"app_version" json:"app_version"`
	TargetChannelCount      int        `db:"target_channel_count" json:"target_channel_count"`
	DeploymentCompletedAt   time.Time  `db:"deployment_completed_at" json:"deployment_completed_at"`
	ObservationStartedAt    time.Time  `db:"observation_started_at" json:"observation_started_at"`
	ObservationEndedAt      time.Time  `db:"observation_ended_at" json:"observation_ended_at"`
	ClosedAt                *time.Time `db:"closed_at" json:"closed_at,omitempty"`
	FinalizedPostBaselineAt *time.Time `db:"finalized_post_baseline_at" json:"finalized_post_baseline_at,omitempty"`
	FinalizedPostCount      int        `db:"finalized_post_count" json:"finalized_post_count"`
	CreatedAt               time.Time  `db:"created_at" json:"created_at"`
	UpdatedAt               time.Time  `db:"updated_at" json:"updated_at"`
}

func (YouTubeCommunityShortsObservationWindow) TableName() string {
	return "youtube_community_shorts_observation_windows"
}

type WatermarkType string

const (
	WatermarkTypeVideo         WatermarkType = "VIDEO"
	WatermarkTypeShort         WatermarkType = "SHORT"
	WatermarkTypeCommunityPost WatermarkType = "COMMUNITY_POST"
)

type YouTubeContentWatermark struct {
	ChannelID     string        `db:"channel_id" json:"channel_id"`
	WatermarkType WatermarkType `db:"watermark_type" json:"watermark_type"`
	Initialized   bool          `db:"initialized" json:"initialized"`
	LastContentID string        `db:"last_content_id" json:"last_content_id,omitempty"`
	UpdatedAt     time.Time     `db:"updated_at" json:"updated_at"`
}

func (YouTubeContentWatermark) TableName() string {
	return "youtube_content_watermarks"
}

type OutboxKind string

const (
	OutboxKindNewVideo      OutboxKind = "NEW_VIDEO"
	OutboxKindNewShort      OutboxKind = "NEW_SHORT"
	OutboxKindLiveStream    OutboxKind = "LIVE_STREAM"
	OutboxKindCommunityPost OutboxKind = "COMMUNITY_POST"
	OutboxKindMilestone     OutboxKind = "MILESTONE"
)

const youtubeNotificationDedupeKeyPrefix = "youtube-notification"

var outboxKindTemplateKeys = map[OutboxKind]TemplateKey{
	OutboxKindNewVideo:      TemplateKeyOutboxVideo,
	OutboxKindNewShort:      TemplateKeyOutboxShorts,
	OutboxKindLiveStream:    TemplateKeyOutboxVideo,
	OutboxKindCommunityPost: TemplateKeyOutboxCommunity,
	OutboxKindMilestone:     TemplateKeyOutboxMilestone,
}

func (k OutboxKind) ToAlarmType() AlarmType {
	switch k {
	case OutboxKindNewVideo, OutboxKindLiveStream, OutboxKindMilestone:
		return AlarmTypeLive
	case OutboxKindNewShort:
		return AlarmTypeShorts
	case OutboxKindCommunityPost:
		return AlarmTypeCommunity
	default:
		return AlarmTypeLive
	}
}

func (k OutboxKind) ToTemplateKey() TemplateKey {
	if templateKey, ok := outboxKindTemplateKeys[k]; ok {
		return templateKey
	}
	return TemplateKeyOutboxVideo
}

type OutboxStatus string

const (
	OutboxStatusPending OutboxStatus = "PENDING"
	OutboxStatusSent    OutboxStatus = "SENT"
	OutboxStatusFailed  OutboxStatus = "FAILED"
)

type YouTubeNotificationOutbox struct {
	ID            int64        `db:"id" json:"id"`
	Kind          OutboxKind   `db:"kind" json:"kind"`
	ChannelID     string       `db:"channel_id" json:"channel_id"`
	ContentID     string       `db:"content_id" json:"content_id"`
	Payload       string       `db:"payload" json:"payload"`
	Status        OutboxStatus `db:"status" json:"status"`
	AttemptCount  int          `db:"attempt_count" json:"attempt_count"`
	NextAttemptAt time.Time    `db:"next_attempt_at" json:"next_attempt_at"`
	CreatedAt     time.Time    `db:"created_at" json:"created_at"`
	LockedAt      *time.Time   `json:"locked_at,omitempty"`
	SentAt        *time.Time   `json:"sent_at,omitempty"`
	Error         string       `db:"error" json:"error,omitempty"`
}

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
func (o *YouTubeNotificationOutbox) DedupeKey() (string, error) {
	if o == nil {
		return "", fmt.Errorf("build youtube notification dedupe key: outbox is nil")
	}
	return BuildYouTubeNotificationDedupeKey(o.Kind, o.ContentID)
}

type LiveStatus string

const (
	LiveStatusUpcoming LiveStatus = "UPCOMING"
	LiveStatusLive     LiveStatus = "LIVE"
	LiveStatusEnded    LiveStatus = "ENDED"
)

type YouTubeLiveSession struct {
	VideoID            string     `db:"video_id" json:"video_id"`
	ChannelID          string     `db:"channel_id" json:"channel_id"`
	Status             LiveStatus `db:"status" json:"status"`
	Title              string     `db:"title" json:"title,omitempty"`
	ScheduledStartTime *time.Time `json:"scheduled_start_time,omitempty"`
	StartedAt          *time.Time `json:"started_at,omitempty"`
	EndedAt            *time.Time `json:"ended_at,omitempty"`
	LiveFirstSeenAt    *time.Time `db:"live_first_seen_at" json:"live_first_seen_at,omitempty"`
	LastSeenAt         time.Time  `db:"last_seen_at" json:"last_seen_at"`
}

func (YouTubeLiveSession) TableName() string {
	return "youtube_live_sessions"
}

type YouTubeLiveViewerSample struct {
	VideoID           string    `db:"video_id" json:"video_id"`
	CapturedAt        time.Time `db:"captured_at" json:"captured_at"`
	ChannelID         string    `db:"channel_id" json:"channel_id"`
	ConcurrentViewers int       `json:"concurrent_viewers"`
}

func (YouTubeLiveViewerSample) TableName() string {
	return "youtube_live_viewer_samples"
}

type YouTubeStreamStats struct {
	VideoID              string     `db:"video_id" json:"video_id"`
	ChannelID            string     `db:"channel_id" json:"channel_id"`
	StartedAt            *time.Time `json:"started_at,omitempty"`
	EndedAt              *time.Time `db:"ended_at" json:"ended_at,omitempty"`
	MaxConcurrentViewers int        `json:"max_concurrent_viewers,omitempty"`
	AvgConcurrentViewers int        `json:"avg_concurrent_viewers,omitempty"`
	SampleCount          int        `db:"sample_count" json:"sample_count"`
	UpdatedAt            time.Time  `db:"updated_at" json:"updated_at"`
}

func (YouTubeStreamStats) TableName() string {
	return "youtube_stream_stats"
}
