package parser

import "time"

type VideoSource string

const (
	VideoSourceUnknown VideoSource = ""
	VideoSourceHTML    VideoSource = "html"
	VideoSourceRSS     VideoSource = "rss"
)

type ReplayStatus uint8

const (
	ReplayStatusUnknown ReplayStatus = iota
	ReplayStatusNotReplay
	ReplayStatusReplay
)

type VideoMetadata struct {
	PublishedAt *time.Time
	Replay      ReplayStatus
}

type ChannelStats struct {
	ChannelID       string `json:"channelId"`
	SubscriberCount int64  `json:"subscriberCount"`
	ViewCount       int64  `json:"viewCount"`
	VideoCount      int64  `json:"videoCount"`
	JoinedDate      int64  `json:"joinedDate"`
	Description     string `json:"description"`
	Country         string `json:"country"`
	Handle          string `json:"handle"`
}

type ChannelSnippet struct {
	Avatar []Thumbnail `json:"avatar"`
	Banner []Thumbnail `json:"banner"`
}

type Thumbnail struct {
	URL    string `json:"url"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

type UpcomingEvent struct {
	VideoID       string      `json:"videoId"`
	Title         string      `json:"title"`
	Thumbnail     []Thumbnail `json:"thumbnail"`
	Status        string      `json:"status"`
	StartTime     *int64      `json:"startTime,omitempty"`
	ViewCountText string      `json:"viewCountText"`
	ChannelTitle  string      `json:"channelTitle"`
}

type Video struct {
	VideoID       string      `json:"videoId"`
	Title         string      `json:"title"`
	Thumbnail     []Thumbnail `json:"thumbnail"`
	ViewCount     int64       `json:"viewCount"`
	PublishedText string      `json:"publishedText"`
	Duration      string      `json:"duration"`
	ChannelID     string      `json:"channelId"`
	ChannelTitle  string      `json:"channelTitle"`
	ChannelHandle string      `json:"channelHandle"`
	Source        VideoSource `json:"-"`
}

type CommunityPost struct {
	PostID         string      `json:"postId"`
	UpstreamPostID string      `json:"upstreamPostId,omitempty"`
	AuthorID       string      `json:"authorId"`
	AuthorName     string      `json:"authorName"`
	AuthorPhoto    []Thumbnail `json:"authorPhoto"`
	ContentText    string      `json:"contentText"`
	PublishedText  string      `json:"publishedText"`
	PublishedAt    *time.Time  `json:"publishedAt,omitempty"`
	LikeCount      int64       `json:"likeCount"`
	CommentCount   int64       `json:"commentCount"`
	Images         []Thumbnail `json:"images,omitempty"`
	VideoID        string      `json:"videoId,omitempty"`
}

type Playlist struct {
	PlaylistID   string      `json:"playlistId"`
	Title        string      `json:"title"`
	Thumbnail    []Thumbnail `json:"thumbnail"`
	VideoCount   int64       `json:"videoCount"`
	ChannelID    string      `json:"channelId"`
	ChannelTitle string      `json:"channelTitle"`
}

type Short struct {
	VideoID     string      `json:"videoId"`
	Title       string      `json:"title"`
	Thumbnail   []Thumbnail `json:"thumbnail"`
	ViewCount   int64       `json:"viewCount"`
	PublishedAt *time.Time  `json:"publishedAt,omitempty"`
}
