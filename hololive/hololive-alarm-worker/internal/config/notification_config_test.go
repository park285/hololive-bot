package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadRuntimeKeepsAlarmShortLinkEnv(t *testing.T) {
	setRuntimeEnv(t)
	t.Setenv("ALARM_SHORT_LINK_BASE_URL", "https://short.holoshi.com")

	config, err := LoadRuntime()
	require.NoError(t, err)
	require.Equal(t, "https://short.holoshi.com", config.Notification.AlarmShortLinkBaseURL)
}
