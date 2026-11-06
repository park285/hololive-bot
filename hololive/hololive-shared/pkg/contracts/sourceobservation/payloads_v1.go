package sourceobservation

import (
	"time"
)

const LiveSnapshotMetadataContractGeneration int64 = 2

const LiveSnapshotQueryContractGeneration int64 = 3

// LiveScheduleObservationMaxAge는 재사용 일정의 원래 관측 시각이 이번 lease 슬롯보다 앞설 수 있는 최대 폭이다.
// 재사용 상한(helper 10분)과 job 대기 상한(1시간)을 넉넉히 덮고, 그보다 오래된 값은 잘못된 시계로 보고 거부한다.
const LiveScheduleObservationMaxAge = 6 * time.Hour

// LiveSnapshotQueryV1은 반환 항목이 아니라 실제 조회한 범위와 완결성을 증명한다.
type LiveSnapshotQueryV1 struct {
	ChannelID        string   `json:"channel_id"`
	Source           string   `json:"source"`
	Statuses         []string `json:"statuses"`
	Exhausted        bool     `json:"exhausted"`
	AccessRestricted bool     `json:"access_restricted"`
	PageCount        int      `json:"page_count"`
}

type VideoListItemV1 struct {
	VideoID      string              `json:"video_id"`
	ChannelID    string              `json:"channel_id"`
	Title        string              `json:"title"`
	PublishedAt  *time.Time          `json:"published_at,omitempty"`
	ScheduledFor *time.Time          `json:"scheduled_for,omitempty"`
	IsPremiere   *bool               `json:"is_premiere,omitempty"`
	Publication  *VideoPublicationV1 `json:"publication,omitempty"`
}

type VideoListV1 struct {
	ChannelID string                `json:"channel_id"`
	Videos    []VideoListItemV1     `json:"videos"`
	Coverage  ChannelListCoverageV1 `json:"coverage"`
}

type ShortsListV1 struct {
	ChannelID string               `json:"channel_id"`
	Videos    []VideoListItemV1    `json:"videos"`
	Coverage  ShortsListCoverageV1 `json:"coverage"`
}

type LiveSessionV1 struct {
	VideoID      string     `json:"video_id"`
	ChannelID    string     `json:"channel_id"`
	Status       string     `json:"status"`
	Title        string     `json:"title,omitempty"`
	TopicID      string     `json:"topic_id,omitempty"`
	ThumbnailURL string     `json:"thumbnail_url,omitempty"`
	ScheduledAt  *time.Time `json:"scheduled_at,omitempty"`
	// ScheduleObservedAt은 ScheduledAt을 이번 슬롯이 아니라 앞선 lease 슬롯에서 확인해 재사용했을 때만 그 슬롯 시각을 싣는다.
	// 없으면 ScheduledAt은 이 관측의 effective time에 확인한 값이다.
	ScheduleObservedAt *time.Time `json:"schedule_observed_at,omitzero"`
	StartedAt          *time.Time `json:"started_at,omitempty"`
	EndedAt            *time.Time `json:"ended_at,omitempty"`
}

type LiveSnapshotV1 struct {
	Sessions []LiveSessionV1         `json:"sessions"`
	Coverage GlobalChannelCoverageV1 `json:"coverage"`
	Query    *LiveSnapshotQueryV1    `json:"query,omitempty"`
}

type FieldValue[T any] struct {
	Present bool `json:"present"`
	Value   T    `json:"value"`
}

type ChannelProfileV1 struct {
	ChannelID   string                   `json:"channel_id"`
	Handle      FieldValue[string]       `json:"handle"`
	Description FieldValue[string]       `json:"description"`
	Country     FieldValue[string]       `json:"country"`
	JoinedDate  FieldValue[string]       `json:"joined_date"`
	Coverage    ChannelProfileCoverageV1 `json:"coverage"`
}

type PhotoVariantV1 struct {
	Kind               string `json:"kind"`
	URL                string `json:"url"`
	Width              int    `json:"width"`
	Height             int    `json:"height"`
	StableMediaID      string `json:"stable_media_id,omitempty"`
	ContentFingerprint string `json:"content_fingerprint,omitempty"`
}

type ChannelPhotoV1 struct {
	ChannelID string                 `json:"channel_id"`
	Variants  []PhotoVariantV1       `json:"variants"`
	Coverage  ChannelPhotoCoverageV1 `json:"coverage"`
}

const (
	MaxScheduleCollaboTalentNames     = 32
	MaxScheduleCollaboTalentNameBytes = 256
)

type ScheduleItemV1 struct {
	ExternalID         string     `json:"external_id"`
	VideoID            string     `json:"video_id,omitempty"`
	ChannelID          string     `json:"channel_id,omitempty"`
	Title              string     `json:"title"`
	ScheduledAt        time.Time  `json:"scheduled_at"`
	EndedAt            *time.Time `json:"ended_at,omitempty"`
	IsLive             bool       `json:"is_live"`
	CollaboTalentNames []string   `json:"collabo_talent_names,omitempty"`
}

type ScheduleSnapshotV1 struct {
	GroupKey string             `json:"group_key"`
	Items    []ScheduleItemV1   `json:"items"`
	Coverage ScheduleCoverageV1 `json:"coverage"`
}
