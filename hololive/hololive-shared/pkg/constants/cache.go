package constants

import "time"

// CacheTTL: 패키지 변수다.
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

// MemberCacheDefaults: 패키지 변수다.
var MemberCacheDefaults = struct {
	ValkeyTTL           time.Duration
	WarmUpChunkSize     int
	WarmUpMaxGoroutines int
}{
	ValkeyTTL:           30 * time.Minute,
	WarmUpChunkSize:     50,
	WarmUpMaxGoroutines: 10,
}

// WebSocketConfig: 패키지 변수다.
var WebSocketConfig = struct {
	MaxReconnectAttempts int
	ReconnectDelay       time.Duration
}{
	MaxReconnectAttempts: 5,
	ReconnectDelay:       5 * time.Second,
}

// ValkeyConfig: 패키지 변수다.
var ValkeyConfig = struct {
	ReadyTimeout      time.Duration
	BlockingPoolSize  int
	PipelineMultiplex int
}{
	ReadyTimeout:      5 * time.Second,
	BlockingPoolSize:  100,
	PipelineMultiplex: 4,
}

// RedisKeys: Redis/Valkey 키 이름 상수입니다.
var RedisKeys = struct {
	AlarmMemberNames string
}{
	AlarmMemberNames: "alarm:member_names",
}
