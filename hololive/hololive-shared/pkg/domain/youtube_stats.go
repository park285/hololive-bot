package domain

import "time"

// TimestampedStats: 특정 시점의 YouTube 채널 통계 (구독자, 비디오 수, 조회수)
type TimestampedStats struct {
	ChannelID       string    `json:"channel_id"`
	MemberName      string    `json:"member_name"`
	SubscriberCount uint64    `json:"subscriber_count"`
	VideoCount      uint64    `json:"video_count"`
	ViewCount       uint64    `json:"view_count"`
	Timestamp       time.Time `json:"timestamp"`
}

// MilestoneType: 달성한 마일스톤의 종류 (구독자 수, 비디오 수 등)
type MilestoneType string

// MilestoneType 상수 목록.
// MilestoneType 상수 목록.
const (
	// MilestoneSubscribers: 구독자 수 달성
	MilestoneSubscribers MilestoneType = "subscribers"
	// MilestoneVideos: 비디오 업로드 수 달성
	MilestoneVideos MilestoneType = "videos"
	// MilestoneViews: 총 조회수 달성
	MilestoneViews MilestoneType = "views"
)

// Milestone: 채널이 달성한 특정 성과(마일스톤) 정보
type Milestone struct {
	ChannelID  string        `json:"channel_id"`
	MemberName string        `json:"member_name"`
	Type       MilestoneType `json:"type"`
	Value      uint64        `json:"value"` // e.g., 1000000 for 1M subscribers
	AchievedAt time.Time     `json:"achieved_at"`
	Notified   bool          `json:"notified"`
}

// StatsChange: 이전 시점 대비 통계 변화량 정보
type StatsChange struct {
	ChannelID        string            `json:"channel_id"`
	MemberName       string            `json:"member_name"`
	SubscriberChange int64             `json:"subscriber_change"`
	VideoChange      int64             `json:"video_change"`
	ViewChange       int64             `json:"view_change"`
	PreviousStats    *TimestampedStats `json:"previous_stats"`
	CurrentStats     *TimestampedStats `json:"current_stats"`
	DetectedAt       time.Time         `json:"detected_at"`
}

// DailySummary: 일일 종합 통계 리포트 (변화량, 달성 마일스톤, 순위 등)
type DailySummary struct {
	Date               time.Time   `json:"date"`
	TotalChanges       int         `json:"total_changes"`
	MilestonesAchieved int         `json:"milestones_achieved"`
	NewVideosDetected  int         `json:"new_videos_detected"`
	TopGainers         []RankEntry `json:"top_gainers"`
	TopUploaders       []RankEntry `json:"top_uploaders"`
}

// RankEntry: 순위 정보의 개별 항목 (채널명, 값, 순위)
type RankEntry struct {
	ChannelID          string `json:"channel_id"`
	MemberName         string `json:"member_name"`
	Value              int64  `json:"value"`               // subscriber change or video count
	CurrentSubscribers uint64 `json:"current_subscribers"` // latest subscriber count (optional)
	Rank               int    `json:"rank"`
}

// TrendData: 특정 기간 동안의 성장 추세 정보
type TrendData struct {
	ChannelID        string    `json:"channel_id"`
	MemberName       string    `json:"member_name"`
	Period           string    `json:"period"` // "daily", "weekly", "monthly"
	SubscriberGrowth int64     `json:"subscriber_growth"`
	VideoUploadRate  float64   `json:"video_upload_rate"` // videos per day
	AvgViewsPerVideo uint64    `json:"avg_views_per_video"`
	UpdatedAt        time.Time `json:"updated_at"`
}
