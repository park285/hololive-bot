package alarmworker

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/config/settings/internal/settingstest"
)

func TestLoadRuntimeRejectsRetiredNotificationEgressEnv(t *testing.T) {
	for _, key := range retiredNotificationEgressEnvKeys {
		t.Run(key, func(t *testing.T) {
			setRuntimeEnv(t)
			t.Setenv(key, "")

			_, err := LoadRuntime()

			require.ErrorContains(t, err, key)
			require.ErrorContains(t, err, "is retired")
		})
	}
}

func TestLoadRuntimeRejectsRetiredNotificationEgressDotEnv(t *testing.T) {
	for _, key := range retiredNotificationEgressEnvKeys {
		t.Run(key, func(t *testing.T) {
			setRuntimeEnv(t)
			settingstest.UnsetEnv(t, key)
			t.Cleanup(func() {
				require.NoError(t, os.Unsetenv(key))
			})

			dir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(dir, ".env"), []byte(key+"=\n"), 0o600))
			t.Chdir(dir)

			_, err := LoadRuntime()

			require.ErrorContains(t, err, key)
			require.ErrorContains(t, err, "is retired")
		})
	}
}

func TestLoadRuntimeKeepsAlarmShortLinkEnv(t *testing.T) {
	setRuntimeEnv(t)
	t.Setenv("ALARM_SHORT_LINK_BASE_URL", "https://short.holoshi.com")

	config, err := LoadRuntime()

	require.NoError(t, err)
	require.Equal(t, "https://short.holoshi.com", config.Notification.AlarmShortLinkBaseURL)
}
