package sourceobservation

import (
	"time"
)

type VideoListItemV1 struct {
	VideoID      string     `json:"video_id"`
	ChannelID    string     `json:"channel_id"`
	Title        string     `json:"title"`
	PublishedAt  *time.Time `json:"published_at,omitempty"`
	ScheduledFor *time.Time `json:"scheduled_for,omitempty"`
	IsPremiere   *bool      `json:"is_premiere,omitempty"`
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
	VideoID     string     `json:"video_id"`
	ChannelID   string     `json:"channel_id"`
	Status      string     `json:"status"`
	ScheduledAt *time.Time `json:"scheduled_at,omitempty"`
	StartedAt   *time.Time `json:"started_at,omitempty"`
	EndedAt     *time.Time `json:"ended_at,omitempty"`
}

type LiveSnapshotV1 struct {
	Sessions []LiveSessionV1         `json:"sessions"`
	Coverage GlobalChannelCoverageV1 `json:"coverage"`
}

type ViewerSampleV1 struct {
	VideoID             string                 `json:"video_id"`
	ViewerCount         *int64                 `json:"viewer_count,omitempty"`
	Availability        string                 `json:"availability"`
	SampleWindowStart   time.Time              `json:"sample_window_start"`
	SampleWindowSeconds int                    `json:"sample_window_seconds"`
	Coverage            ViewerSampleCoverageV1 `json:"coverage"`
}

type ChannelStatsV1 struct {
	ChannelID       string                 `json:"channel_id"`
	SubscriberCount *int64                 `json:"subscriber_count,omitempty"`
	ViewCount       *int64                 `json:"view_count,omitempty"`
	VideoCount      *int64                 `json:"video_count,omitempty"`
	Coverage        ChannelStatsCoverageV1 `json:"coverage"`
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
