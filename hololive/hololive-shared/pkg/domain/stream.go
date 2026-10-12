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
	"time"

	"github.com/kapu/hololive-shared/pkg/timeutil"
)

type StreamStatus string

// StreamStatus 상수 목록.
const (
	// StreamStatusLive: 방송 진행 중.
	StreamStatusLive StreamStatus = "live"
	// StreamStatusUpcoming: 방송 예정.
	StreamStatusUpcoming StreamStatus = "upcoming"
	// StreamStatusPast: 방송 종료됨.
	StreamStatusPast StreamStatus = "past"
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

// 이미 시작 시간이 지났거나 예정 시간이 없으면 nil을 반환한다.
func (s *Stream) TimeUntilStart() *time.Duration {
	if s.StartScheduled == nil {
		return nil
	}

	now := time.Now()
	if s.StartScheduled.Before(now) {
		return nil
	}

	duration := s.StartScheduled.Sub(now)

	return new(duration)
}

func (s *Stream) MinutesUntilStart() int {
	return timeutil.MinutesUntilFloorPtr(s.StartScheduled, time.Now())
}

func (s *Stream) HasYouTubeInfo() bool {
	return s.ID != ""
}
