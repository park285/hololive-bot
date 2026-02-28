package domain

import "time"

// NextStreamStatus: 다음 방송 정보 조회 결과 상태 (라이브, 예정, 없음 등)
type NextStreamStatus string

// NextStreamStatus 상수 목록.
// NextStreamStatus 상수 목록.
const (
	// NextStreamStatusLive: 현재 방송 중
	NextStreamStatusLive NextStreamStatus = "live"
	// NextStreamStatusUpcoming: 방송 예정 정보 있음
	NextStreamStatusUpcoming NextStreamStatus = "upcoming"
	// NextStreamStatusNoUpcoming: 예정된 방송이 없음
	NextStreamStatusNoUpcoming NextStreamStatus = "no_upcoming"
	// NextStreamStatusTimeUnknown: 방송 예정이나 시작 시간을 알 수 없음
	NextStreamStatusTimeUnknown NextStreamStatus = "time_unknown"
)

func (s NextStreamStatus) String() string {
	return string(s)
}

// IsLive: 상태가 '현재 방송 중'인지 확인합니다.
func (s NextStreamStatus) IsLive() bool {
	return s == NextStreamStatusLive
}

// IsUpcoming: 상태가 '방송 예정'인지 확인합니다.
func (s NextStreamStatus) IsUpcoming() bool {
	return s == NextStreamStatusUpcoming
}

// IsValid: 상태 값이 정의된 목록에 포함되는 유효한 값인지 확인합니다.
func (s NextStreamStatus) IsValid() bool {
	switch s {
	case NextStreamStatusLive, NextStreamStatusUpcoming, NextStreamStatusNoUpcoming, NextStreamStatusTimeUnknown:
		return true
	default:
		return false
	}
}

// NextStreamInfo: 다음 방송에 대한 요약 정보
type NextStreamInfo struct {
	Status         NextStreamStatus
	VideoID        string
	Title          string
	StartScheduled *time.Time
}
