package config

import "time"

// NotificationConfig: 방송 알림 스케줄링(미리 알림 시간, 체크 주기) 설정
type NotificationConfig struct {
	AdvanceMinutes []int
	CheckInterval  time.Duration
}
