package settings

import (
	"time"

	sharedenv "github.com/park285/shared-go/pkg/envutil"
)

func loadAlarmDispatchRetentionConfig() AlarmDispatchRetentionConfig {
	return AlarmDispatchRetentionConfig{
		Enabled:         sharedenv.Bool("ALARM_DISPATCH_RETENTION_ENABLED", true),
		Interval:        positiveDurationMS("ALARM_DISPATCH_RETENTION_INTERVAL_MS", time.Hour),
		QueryTimeout:    positiveDurationMS("ALARM_DISPATCH_RETENTION_QUERY_TIMEOUT_MS", 30*time.Second),
		Limit:           alarmDispatchRetentionLimit(),
		SentDays:        positiveIntEnv("ALARM_DISPATCH_RETENTION_SENT_DAYS", 90),
		DLQDays:         positiveIntEnv("ALARM_DISPATCH_RETENTION_DLQ_DAYS", 180),
		QuarantinedDays: positiveIntEnv("ALARM_DISPATCH_RETENTION_QUARANTINED_DAYS", 180),
		CancelledDays:   positiveIntEnv("ALARM_DISPATCH_RETENTION_CANCELLED_DAYS", 90),
		EventDays:       positiveIntEnv("ALARM_DISPATCH_RETENTION_EVENT_DAYS", 90),
	}
}

func alarmDispatchRetentionLimit() int {
	return min(positiveIntEnv("ALARM_DISPATCH_RETENTION_LIMIT", 1000), 10000)
}

func positiveDurationMS(key string, defaultValue time.Duration) time.Duration {
	value := positiveIntEnv(key, 0)
	if value == 0 {
		return defaultValue
	}
	return time.Duration(value) * time.Millisecond
}
