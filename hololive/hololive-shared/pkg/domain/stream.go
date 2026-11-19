package domain

import (
	"time"

	"github.com/kapu/hololive-shared/pkg/timeutil"
)

type StreamStatus string

const (
	StreamStatusLive     StreamStatus = "live"
	StreamStatusUpcoming StreamStatus = "upcoming"
	StreamStatusPast     StreamStatus = "past"
)

func (s StreamStatus) String() string {
	return string(s)
}

func (s StreamStatus) IsValid() bool {
	switch s {
	case StreamStatusLive, StreamStatusUpcoming, StreamStatusPast:
		return true
	default:
		return false
	}
}

type Stream struct {
	ID                 string       `json:"id"`
	Title              string       `json:"title"`
	ChannelID          string       `json:"channel_id"`
	ChannelName        string       `json:"channel_name"`
	Status             StreamStatus `json:"status"`
	StartScheduled     *time.Time   `json:"start_scheduled,omitempty"`
	StartActual        *time.Time   `json:"start_actual,omitempty"`
	Duration           *int         `json:"duration,omitempty"`
	Thumbnail          *string      `json:"thumbnail,omitempty"`
	Link               *string      `json:"link,omitempty"`
	TopicID            *string      `json:"topic_id,omitempty"`
	Channel            *Channel     `json:"channel,omitempty"`
	ViewerCount        *int         `json:"viewer_count,omitempty"`
	IsPremiere         bool         `json:"is_premiere"`
	CollaboTalentNames []string     `json:"collabo_talent_names,omitempty"`
}

func (s *Stream) IsLive() bool {
	return s.Status == StreamStatusLive
}

func (s *Stream) IsUpcoming() bool {
	return s.Status == StreamStatusUpcoming
}

func (s *Stream) IsPast() bool {
	return s.Status == StreamStatusPast
}

func (s *Stream) GetYouTubeURL() string {
	if s.Link != nil && *s.Link != "" {
		return *s.Link
	}

	return YouTubeWatchURL(s.ID)
}

func (s *Stream) MinutesUntilStart() int {
	return timeutil.MinutesUntilFloorPtr(s.StartScheduled, time.Now())
}

func (s *Stream) HasYouTubeInfo() bool {
	return s.ID != ""
}
