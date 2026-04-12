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

package constants

import "time"

var CacheTTL = struct {
	LiveStreams        time.Duration
	UpcomingStreams    time.Duration
	ChannelSchedule    time.Duration
	ChannelInfo        time.Duration
	ChannelSearch      time.Duration
	NextStreamInfo     time.Duration
	NotificationSent   time.Duration
	TwitchNotification time.Duration
}{
	LiveStreams:        5 * time.Minute,  // 5분 - 라이브 스트림 목록
	UpcomingStreams:    5 * time.Minute,  // 5분 - 예정 스트림 목록
	ChannelSchedule:    5 * time.Minute,  // 5분 - 채널 스케줄
	ChannelInfo:        20 * time.Minute, // 20분 - 채널 정보
	ChannelSearch:      10 * time.Minute, // 10분 - 채널 검색 결과
	NextStreamInfo:     60 * time.Minute, // 1시간 - 다음 방송 정보
	NotificationSent:   24 * time.Hour,   // 24시간 - 알림 발송 기록
	TwitchNotification: 168 * time.Hour,  // 7일 - Twitch 알림 발송 기록 (stream_id 기반)
}

var MemberCacheDefaults = struct {
	ValkeyTTL           time.Duration
	WarmUpChunkSize     int
	WarmUpMaxGoroutines int
}{
	ValkeyTTL:           30 * time.Minute,
	WarmUpChunkSize:     50,
	WarmUpMaxGoroutines: 10,
}

var WebSocketConfig = struct {
	MaxReconnectAttempts int
	ReconnectDelay       time.Duration
}{
	MaxReconnectAttempts: 5,
	ReconnectDelay:       5 * time.Second,
}

var ValkeyConfig = struct {
	ReadyTimeout      time.Duration
	BlockingPoolSize  int
	PipelineMultiplex int
}{
	ReadyTimeout:      5 * time.Second,
	BlockingPoolSize:  100,
	PipelineMultiplex: 4,
}

var RedisKeys = struct {
	AlarmMemberNames string
}{
	AlarmMemberNames: "alarm:member_names",
}
