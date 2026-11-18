package settings

import "time"

type NotificationConfig struct {
	AdvanceMinutes        []int
	CheckInterval         time.Duration
	AlarmShortLinkBaseURL string
}
