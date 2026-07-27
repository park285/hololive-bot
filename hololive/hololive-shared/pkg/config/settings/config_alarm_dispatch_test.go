package settings

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestLoadAlarmDispatchRetentionConfigDefaults(t *testing.T) {
	config := loadAlarmDispatchRetentionConfig()

	assert.Equal(t, AlarmDispatchRetentionConfig{
		Enabled:         true,
		Interval:        time.Hour,
		QueryTimeout:    30 * time.Second,
		Limit:           1000,
		SentDays:        90,
		DLQDays:         180,
		QuarantinedDays: 180,
		CancelledDays:   90,
		EventDays:       90,
	}, config)
}

func TestLoadAlarmDispatchRetentionConfigFromEnvironment(t *testing.T) {
	t.Setenv("ALARM_DISPATCH_RETENTION_ENABLED", "false")
	t.Setenv("ALARM_DISPATCH_RETENTION_INTERVAL_MS", "2000")
	t.Setenv("ALARM_DISPATCH_RETENTION_QUERY_TIMEOUT_MS", "3000")
	t.Setenv("ALARM_DISPATCH_RETENTION_LIMIT", "400")
	t.Setenv("ALARM_DISPATCH_RETENTION_SENT_DAYS", "50")
	t.Setenv("ALARM_DISPATCH_RETENTION_DLQ_DAYS", "60")
	t.Setenv("ALARM_DISPATCH_RETENTION_QUARANTINED_DAYS", "70")
	t.Setenv("ALARM_DISPATCH_RETENTION_CANCELLED_DAYS", "80")
	t.Setenv("ALARM_DISPATCH_RETENTION_EVENT_DAYS", "90")

	config := loadAlarmDispatchRetentionConfig()

	assert.Equal(t, AlarmDispatchRetentionConfig{
		Enabled:         false,
		Interval:        2 * time.Second,
		QueryTimeout:    3 * time.Second,
		Limit:           400,
		SentDays:        50,
		DLQDays:         60,
		QuarantinedDays: 70,
		CancelledDays:   80,
		EventDays:       90,
	}, config)
}

func TestLoadAlarmDispatchRetentionConfigFallsBackForInvalidValues(t *testing.T) {
	for _, value := range []string{"0", "-1", "invalid"} {
		t.Run(value, func(t *testing.T) {
			for _, key := range []string{
				"ALARM_DISPATCH_RETENTION_INTERVAL_MS",
				"ALARM_DISPATCH_RETENTION_QUERY_TIMEOUT_MS",
				"ALARM_DISPATCH_RETENTION_LIMIT",
				"ALARM_DISPATCH_RETENTION_SENT_DAYS",
				"ALARM_DISPATCH_RETENTION_DLQ_DAYS",
				"ALARM_DISPATCH_RETENTION_QUARANTINED_DAYS",
				"ALARM_DISPATCH_RETENTION_CANCELLED_DAYS",
				"ALARM_DISPATCH_RETENTION_EVENT_DAYS",
			} {
				t.Setenv(key, value)
			}

			config := loadAlarmDispatchRetentionConfig()

			assert.Equal(t, time.Hour, config.Interval)
			assert.Equal(t, 30*time.Second, config.QueryTimeout)
			assert.Equal(t, 1000, config.Limit)
			assert.Equal(t, 90, config.SentDays)
			assert.Equal(t, 180, config.DLQDays)
			assert.Equal(t, 180, config.QuarantinedDays)
			assert.Equal(t, 90, config.CancelledDays)
			assert.Equal(t, 90, config.EventDays)
		})
	}
}

func TestLoadAlarmDispatchRetentionConfigClampsLimit(t *testing.T) {
	t.Setenv("ALARM_DISPATCH_RETENTION_LIMIT", "10001")

	config := loadAlarmDispatchRetentionConfig()

	assert.Equal(t, 10000, config.Limit)
}
