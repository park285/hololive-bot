package scraping

import (
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper/internal/scraping/parser"
)

type ChannelStats = parser.ChannelStats
type ChannelSnippet = parser.ChannelSnippet
type Thumbnail = parser.Thumbnail
type UpcomingEvent = parser.UpcomingEvent
type Video = parser.Video
type VideoSource = parser.VideoSource
type ReplayStatus = parser.ReplayStatus
type VideoMetadata = parser.VideoMetadata
type CommunityPost = parser.CommunityPost
type Playlist = parser.Playlist
type Short = parser.Short

const (
	VideoSourceUnknown    = parser.VideoSourceUnknown
	VideoSourceHTML       = parser.VideoSourceHTML
	VideoSourceRSS        = parser.VideoSourceRSS
	ReplayStatusUnknown   = parser.ReplayStatusUnknown
	ReplayStatusNotReplay = parser.ReplayStatusNotReplay
	ReplayStatusReplay    = parser.ReplayStatusReplay
)
