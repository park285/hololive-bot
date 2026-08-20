package settings

import (
	"time"

	sharedenv "github.com/park285/shared-go/pkg/envutil"
)

func loadAlarmDispatchRetentionConfig() (AlarmDispatchRetentionConfig, error) {
	config := AlarmDispatchRetentionConfig{
		Enabled: sharedenv.Bool("ALARM_DISPATCH_RETENTION_ENABLED", true),
	}
	var err error
	if config.Interval, err = requiredMillisDurationEnv("ALARM_DISPATCH_RETENTION_INTERVAL_MS", time.Hour); err != nil {
		return AlarmDispatchRetentionConfig{}, err
	}
	if config.QueryTimeout, err = requiredMillisDurationEnv("ALARM_DISPATCH_RETENTION_QUERY_TIMEOUT_MS", 30*time.Second); err != nil {
		return AlarmDispatchRetentionConfig{}, err
	}
	if config.Limit, err = alarmDispatchRetentionLimit(); err != nil {
		return AlarmDispatchRetentionConfig{}, err
	}
	for _, field := range []struct {
		key      string
		fallback int
		target   *int
	}{
		{key: "ALARM_DISPATCH_RETENTION_SENT_DAYS", fallback: 90, target: &config.SentDays},
		{key: "ALARM_DISPATCH_RETENTION_DLQ_DAYS", fallback: 180, target: &config.DLQDays},
		{key: "ALARM_DISPATCH_RETENTION_QUARANTINED_DAYS", fallback: 180, target: &config.QuarantinedDays},
		{key: "ALARM_DISPATCH_RETENTION_CANCELLED_DAYS", fallback: 90, target: &config.CancelledDays},
		{key: "ALARM_DISPATCH_RETENTION_EVENT_DAYS", fallback: 90, target: &config.EventDays},
	} {
		if *field.target, err = requiredPositiveIntEnv(field.key, field.fallback); err != nil {
			return AlarmDispatchRetentionConfig{}, err
		}
	}
	return config, nil
}

func alarmDispatchRetentionLimit() (int, error) {
	limit, err := requiredPositiveIntEnv("ALARM_DISPATCH_RETENTION_LIMIT", 1000)
	if err != nil {
		return 0, err
	}
	return min(limit, 10000), nil
}
