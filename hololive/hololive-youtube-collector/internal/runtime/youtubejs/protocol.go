package youtubejs

import (
	"time"

	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper/scraping/parser"
)

type Pagination struct {
	PageCount   int    `json:"page_count"`
	CursorStart string `json:"cursor_start,omitempty"`
	CursorEnd   string `json:"cursor_end,omitempty"`
	Exhausted   bool   `json:"exhausted"`
	Continuity  string `json:"continuity"`
}

type CommunityRequest struct {
	ChannelID         string `json:"channel_id"`
	MaxResults        int    `json:"max_results"`
	MaxPages          int    `json:"max_pages"`
	MaxAggregateBytes int    `json:"max_aggregate_bytes"`
	ProxyURL          string `json:"proxy_url,omitempty"`
}

type CommunityResult struct {
	Posts []*parser.CommunityPost `json:"posts"`
	Pagination
	MissingTab bool   `json:"missing_tab,omitempty"`
	Error      string `json:"error,omitempty"`
}

type ContentRequest struct {
	ChannelID         string `json:"channel_id"`
	Kind              string `json:"kind"`
	MaxResults        int    `json:"max_results"`
	MaxPages          int    `json:"max_pages"`
	MaxAggregateBytes int    `json:"max_aggregate_bytes"`
	ProxyURL          string `json:"proxy_url,omitempty"`
}

type ContentItem struct {
	VideoID      string     `json:"video_id"`
	ChannelID    string     `json:"channel_id"`
	Title        string     `json:"title"`
	PublishedAt  *time.Time `json:"published_at,omitempty"`
	ScheduledFor *time.Time `json:"scheduled_for,omitempty"`
}

type ContentResult struct {
	Items []ContentItem `json:"items"`
	Pagination
	MissingTab bool   `json:"missing_tab,omitempty"`
	Error      string `json:"error,omitempty"`
}

type ChannelRequest struct {
	ChannelID         string `json:"channel_id"`
	MaxPages          int    `json:"max_pages"`
	MaxAggregateBytes int    `json:"max_aggregate_bytes"`
	ProxyURL          string `json:"proxy_url,omitempty"`
}

type LiveSessionItem struct {
	VideoID     string     `json:"video_id"`
	ChannelID   string     `json:"channel_id"`
	Status      string     `json:"status"`
	ScheduledAt *time.Time `json:"scheduled_at,omitempty"`
	StartedAt   *time.Time `json:"started_at,omitempty"`
	EndedAt     *time.Time `json:"ended_at,omitempty"`
}

type ChannelStatsItem struct {
	SubscriberCount *int64 `json:"subscriber_count"`
	ViewCount       *int64 `json:"view_count"`
	VideoCount      *int64 `json:"video_count"`
}

type ChannelProfileItem struct {
	Handle      *string `json:"handle"`
	Description *string `json:"description"`
	Country     *string `json:"country"`
	JoinedDate  *string `json:"joined_date"`
}

type ChannelPhotoVariant struct {
	Kind   string `json:"kind"`
	URL    string `json:"url"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

type ChannelResult struct {
	LiveSessions []LiveSessionItem     `json:"live_sessions"`
	Stats        ChannelStatsItem      `json:"stats"`
	Profile      ChannelProfileItem    `json:"profile"`
	Photo        []ChannelPhotoVariant `json:"photo"`
	Pagination
	MissingTab bool   `json:"missing_tab,omitempty"`
	Error      string `json:"error,omitempty"`
}

type ViewerRequest struct {
	VideoID           string `json:"video_id"`
	MaxAggregateBytes int    `json:"max_aggregate_bytes"`
	ProxyURL          string `json:"proxy_url,omitempty"`
}

type ViewerResult struct {
	VideoID      string `json:"video_id"`
	ViewerCount  *int64 `json:"viewer_count"`
	Availability string `json:"availability"`
	Pagination
	Error string `json:"error,omitempty"`
}
