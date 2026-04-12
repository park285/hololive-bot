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

import "time"

type TimestampedStats struct {
	ChannelID       string    `json:"channel_id"`
	MemberName      string    `json:"member_name"`
	SubscriberCount uint64    `json:"subscriber_count"`
	VideoCount      uint64    `json:"video_count"`
	ViewCount       uint64    `json:"view_count"`
	Timestamp       time.Time `json:"timestamp"`
}

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

type Milestone struct {
	ChannelID  string        `json:"channel_id"`
	MemberName string        `json:"member_name"`
	Type       MilestoneType `json:"type"`
	Value      uint64        `json:"value"` // e.g., 1000000 for 1M subscribers
	AchievedAt time.Time     `json:"achieved_at"`
	Notified   bool          `json:"notified"`
}

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

type DailySummary struct {
	Date               time.Time   `json:"date"`
	TotalChanges       int         `json:"total_changes"`
	MilestonesAchieved int         `json:"milestones_achieved"`
	NewVideosDetected  int         `json:"new_videos_detected"`
	TopGainers         []RankEntry `json:"top_gainers"`
	TopUploaders       []RankEntry `json:"top_uploaders"`
}

type RankEntry struct {
	ChannelID          string `json:"channel_id"`
	MemberName         string `json:"member_name"`
	Value              int64  `json:"value"`               // subscriber change or video count
	CurrentSubscribers uint64 `json:"current_subscribers"` // latest subscriber count (optional)
	Rank               int    `json:"rank"`
}

type TrendData struct {
	ChannelID        string    `json:"channel_id"`
	MemberName       string    `json:"member_name"`
	Period           string    `json:"period"` // "daily", "weekly", "monthly"
	SubscriberGrowth int64     `json:"subscriber_growth"`
	VideoUploadRate  float64   `json:"video_upload_rate"` // videos per day
	AvgViewsPerVideo uint64    `json:"avg_views_per_video"`
	UpdatedAt        time.Time `json:"updated_at"`
}
