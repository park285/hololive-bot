package alarmworker

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var alarmDispatchRetentionEnvKeys = []string{
	"ALARM_DISPATCH_RETENTION_INTERVAL_MS",
	"ALARM_DISPATCH_RETENTION_QUERY_TIMEOUT_MS",
	"ALARM_DISPATCH_RETENTION_LIMIT",
	"ALARM_DISPATCH_RETENTION_SENT_DAYS",
	"ALARM_DISPATCH_RETENTION_DLQ_DAYS",
	"ALARM_DISPATCH_RETENTION_QUARANTINED_DAYS",
	"ALARM_DISPATCH_RETENTION_CANCELLED_DAYS", //nolint:misspell // ALARM_DISPATCH_RETENTION_CANCELLED_DAYS는 배포 환경에 실재하는 환경변수 이름이라 US 철자로 바꾸면 설정이 끊긴다.
	"ALARM_DISPATCH_RETENTION_EVENT_DAYS",
}

func TestLoadAlarmDispatchRetentionConfigDefaults(t *testing.T) {
	config, err := loadDispatchRetentionConfig()
	require.NoError(t, err)

	assert.Equal(t, DispatchRetentionConfig{
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
	t.Setenv("ALARM_DISPATCH_RETENTION_CANCELLED_DAYS", "80") //nolint:misspell // ALARM_DISPATCH_RETENTION_CANCELLED_DAYS는 배포 환경에 실재하는 환경변수 이름이라 US 철자로 바꾸면 설정이 끊긴다.
	t.Setenv("ALARM_DISPATCH_RETENTION_EVENT_DAYS", "90")

	config, err := loadDispatchRetentionConfig()
	require.NoError(t, err)

	assert.Equal(t, DispatchRetentionConfig{
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

func TestLoadAlarmDispatchRetentionConfigRejectsInvalidValues(t *testing.T) {
	for _, key := range alarmDispatchRetentionEnvKeys {
		for _, value := range []string{"0", "-1", "invalid", ""} {
			t.Run(key+"="+value, func(t *testing.T) {
				t.Setenv(key, value)

				_, err := loadDispatchRetentionConfig()

				require.Error(t, err)
				assert.Contains(t, err.Error(), key)
			})
		}
	}
}

func TestLoadAlarmDispatchRetentionConfigClampsLimit(t *testing.T) {
	t.Setenv("ALARM_DISPATCH_RETENTION_LIMIT", "10001")

	config, err := loadDispatchRetentionConfig()
	require.NoError(t, err)

	assert.Equal(t, 10000, config.Limit)
}
