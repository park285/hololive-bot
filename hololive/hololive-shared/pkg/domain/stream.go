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
	"math"
	"time"
)

// StreamStatus: 방송 상태(진행 중, 예정, 종료)를 나타내는 열거형
type StreamStatus string

// StreamStatus 상수 목록.
const (
	// StreamStatusLive: 방송 진행 중
	StreamStatusLive StreamStatus = "live"
	// StreamStatusUpcoming: 방송 예정
	StreamStatusUpcoming StreamStatus = "upcoming"
	// StreamStatusPast: 방송 종료됨
	StreamStatusPast StreamStatus = "past"
)

func (s StreamStatus) String() string {
	return string(s)
}

// IsValid: 방송 상태 값이 유효한지 검증합니다.
func (s StreamStatus) IsValid() bool {
	switch s {
	case StreamStatusLive, StreamStatusUpcoming, StreamStatusPast:
		return true
	default:
		return false
	}
}

// Stream: Holodex 등에서 수집한 방송(스트림) 상세 정보
type Stream struct {
	ID             string       `json:"id"`
	Title          string       `json:"title"`
	ChannelID      string       `json:"channel_id"`
	ChannelName    string       `json:"channel_name"`
	Status         StreamStatus `json:"status"`
	StartScheduled *time.Time   `json:"start_scheduled,omitempty"`
	StartActual    *time.Time   `json:"start_actual,omitempty"`
	Duration       *int         `json:"duration,omitempty"`
	Thumbnail      *string      `json:"thumbnail,omitempty"`
	Link           *string      `json:"link,omitempty"`
	TopicID        *string      `json:"topic_id,omitempty"`
	Channel        *Channel     `json:"channel,omitempty"`
	ViewerCount    *int         `json:"viewer_count,omitempty"`

	// Chzzk 관련 필드
	ChzzkChannelID string `json:"chzzk_channel_id,omitempty"` // Chzzk 채널 ID
	ChzzkLiveID    int    `json:"chzzk_live_id,omitempty"`    // Chzzk 예약 방송 ID (0이면 예고 없는 OPEN)
	ChzzkLiveURL   string `json:"chzzk_live_url,omitempty"`   // Chzzk Live URL
	IsIntegrated   bool   `json:"is_integrated,omitempty"`    // 동시 송출 여부 (YouTube + Chzzk)
	IsChzzkOnly    bool   `json:"is_chzzk_only,omitempty"`    // Chzzk 단독 여부

	// Twitch 관련 필드
	TwitchUserID    string `json:"twitch_user_id,omitempty"`    // Twitch User ID
	TwitchUserLogin string `json:"twitch_user_login,omitempty"` // Twitch User Login (lowercase)
	TwitchStreamID  string `json:"twitch_stream_id,omitempty"`  // Twitch Stream ID
	TwitchLiveURL   string `json:"twitch_live_url,omitempty"`   // Twitch Live URL
	IsTwitchOnly    bool   `json:"is_twitch_only,omitempty"`    // Twitch 단독 여부
}

// IsLive: 방송이 현재 진행 중('live')인지 확인합니다.
func (s *Stream) IsLive() bool {
	return s.Status == StreamStatusLive
}

// IsUpcoming: 방송이 예정('upcoming') 상태인지 확인합니다.
func (s *Stream) IsUpcoming() bool {
	return s.Status == StreamStatusUpcoming
}

// IsPast: 방송이 종료('past')되었는지 확인합니다.
func (s *Stream) IsPast() bool {
	return s.Status == StreamStatusPast
}

// GetYouTubeURL: 방송 시청을 위한 YouTube URL을 반환한다. (Link 필드가 없으면 ID로 생성)
func (s *Stream) GetYouTubeURL() string {
	if s.Link != nil && *s.Link != "" {
		return *s.Link
	}
	return "https://youtube.com/watch?v=" + s.ID
}

// TimeUntilStart: 예정된 방송 시작 시각까지 남은 시간을 Duration으로 반환합니다.
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

// MinutesUntilStart: 방송 시작까지 남은 시간을 '분' 단위(내림)로 계산하여 반환합니다.
func (s *Stream) MinutesUntilStart() int {
	return minutesUntilFloor(s.StartScheduled, time.Now())
}

// minutesUntilFloor: target 시각까지 남은 시간을 내림(분 단위)으로 반환합니다.
// target이 nil이거나 reference보다 이전이면 -1을 반환합니다.
//
// 내림 사용 이유: 카카오톡은 메시지 도착 시각을 분 단위 절삭으로 표시하므로,
// 올림 시 "N+1분 전"으로 보이는 UX 이슈가 발생한다.
func minutesUntilFloor(target *time.Time, reference time.Time) int {
	if target == nil {
		return -1
	}
	if target.Before(reference) {
		return -1
	}
	duration := target.Sub(reference)
	minutesUntil := math.Floor(duration.Minutes())
	if minutesUntil < 0 {
		return -1
	}
	return int(minutesUntil)
}

// GetChzzkLiveURL: Chzzk Live URL을 반환합니다. (비어있으면 빈 문자열)
func (s *Stream) GetChzzkLiveURL() string {
	return s.ChzzkLiveURL
}

// GetTwitchLiveURL: Twitch Live URL을 반환합니다. (비어있으면 빈 문자열)
func (s *Stream) GetTwitchLiveURL() string {
	return s.TwitchLiveURL
}

// HasYouTubeInfo: YouTube 정보(ID)가 있는지 확인합니다.
func (s *Stream) HasYouTubeInfo() bool {
	return s.ID != ""
}
