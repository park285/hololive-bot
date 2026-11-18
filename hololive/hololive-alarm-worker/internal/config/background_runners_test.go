package config

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/config/settingstest"
)

var backgroundRunnerEnvKeys = []string{
	celebrationRunnerEnabledEnv,
	"CELEBRATION_CHECK_HOUR_KST",
	"CELEBRATION_RUN_INTERVAL_MS",
	"X_SPACES_CONFIG_FILE",
	"X_SPACES_KEY_FILE",
}

func setBackgroundRunnerRuntimeEnv(t *testing.T) {
	t.Helper()
	setRuntimeEnv(t)

	for _, key := range backgroundRunnerEnvKeys {
		settingstest.UnsetEnv(t, key)
	}
}

func TestHourOfDayEnvReadsValidValues(t *testing.T) {
	for value, want := range map[string]int{"0": 0, "23": 23} {
		t.Setenv("TEST_WORKER_CONFIG_HOUR", value)

		hour, err := hourOfDayEnv("TEST_WORKER_CONFIG_HOUR", 9)
		require.NoError(t, err, value)
		assert.Equal(t, want, hour, value)
	}
}

func TestHourOfDayEnvUsesDefaultWhenUnset(t *testing.T) {
	settingstest.UnsetEnv(t, "TEST_WORKER_CONFIG_HOUR")

	hour, err := hourOfDayEnv("TEST_WORKER_CONFIG_HOUR", 9)
	require.NoError(t, err)
	assert.Equal(t, 9, hour)
}

func TestHourOfDayEnvRejectsInvalidValues(t *testing.T) {
	for _, value := range []string{"", "  ", "noon", "-1", "24", "99"} {
		t.Setenv("TEST_WORKER_CONFIG_HOUR", value)

		_, err := hourOfDayEnv("TEST_WORKER_CONFIG_HOUR", 0)
		require.Error(t, err, value)
		assert.Contains(t, err.Error(), "TEST_WORKER_CONFIG_HOUR", value)
	}
}

func TestLoadRuntimeRejectsInvalidBackgroundRunnerSwitch(t *testing.T) {
	setBackgroundRunnerRuntimeEnv(t)
	t.Setenv(celebrationRunnerEnabledEnv, "maybe")

	if _, err := LoadRuntime(); err == nil || !strings.Contains(err.Error(), celebrationRunnerEnabledEnv) {
		t.Fatalf("LoadRuntime() error = %v, want invalid %s rejection", err, celebrationRunnerEnabledEnv)
	}
}

func TestLoadRuntimeValidatesBackgroundRunnerConfigOnlyWhenEnabled(t *testing.T) {
	for _, tc := range []struct {
		switchKey, key, value string
	}{
		{switchKey: celebrationRunnerEnabledEnv, key: "CELEBRATION_CHECK_HOUR_KST", value: "noon"},
		{switchKey: celebrationRunnerEnabledEnv, key: "CELEBRATION_CHECK_HOUR_KST", value: "24"},
		{switchKey: celebrationRunnerEnabledEnv, key: "CELEBRATION_RUN_INTERVAL_MS", value: "0"},
		{switchKey: celebrationRunnerEnabledEnv, key: "CELEBRATION_RUN_INTERVAL_MS", value: " "},
	} {
		t.Run(tc.key+"="+tc.value, func(t *testing.T) {
			setBackgroundRunnerRuntimeEnv(t)
			t.Setenv(tc.key, tc.value)

			t.Setenv(tc.switchKey, "false")

			config, err := LoadRuntime()
			require.NoError(t, err, "disabled runner must not read %s", tc.key)
			assert.Equal(t, CelebrationRunnerConfig{}, config.Celebration)

			t.Setenv(tc.switchKey, "true")

			if _, err := LoadRuntime(); err == nil || !strings.Contains(err.Error(), tc.key) {
				t.Fatalf("LoadRuntime() error = %v, want invalid %s rejection", err, tc.key)
			}
		})
	}
}

func TestLoadRuntimeReadsEnabledBackgroundRunnerConfig(t *testing.T) {
	t.Run("defaults", func(t *testing.T) {
		setBackgroundRunnerRuntimeEnv(t)
		t.Setenv(celebrationRunnerEnabledEnv, "true")

		config, err := LoadRuntime()
		require.NoError(t, err)
		assert.Equal(t, CelebrationRunnerConfig{Enabled: true, CheckHourKST: 0, RunInterval: time.Hour}, config.Celebration)
	})

	t.Run("boundaries", func(t *testing.T) {
		setBackgroundRunnerRuntimeEnv(t)
		t.Setenv(celebrationRunnerEnabledEnv, "true")
		t.Setenv("CELEBRATION_CHECK_HOUR_KST", "23")
		t.Setenv("CELEBRATION_RUN_INTERVAL_MS", "1")

		config, err := LoadRuntime()
		require.NoError(t, err)
		assert.Equal(t, CelebrationRunnerConfig{Enabled: true, CheckHourKST: 23, RunInterval: time.Millisecond}, config.Celebration)
	})
}

func TestLoadRuntimeXSpacesPaths(t *testing.T) {
	const (
		configFile = "/run/hololive-bot/x-spaces/config.json"
		keyFile    = "/run/hololive-bot/x-spaces/key"
	)

	setBackgroundRunnerRuntimeEnv(t)

	config, err := LoadRuntime()
	require.NoError(t, err)
	assert.Equal(t, XSpacesConfig{}, config.XSpaces)

	t.Setenv("X_SPACES_KEY_FILE", keyFile)

	config, err = LoadRuntime()
	require.NoError(t, err)
	assert.Equal(t, XSpacesConfig{KeyFile: keyFile}, config.XSpaces)

	t.Setenv("X_SPACES_CONFIG_FILE", configFile)

	config, err = LoadRuntime()
	require.NoError(t, err)
	assert.Equal(t, XSpacesConfig{ConfigFile: configFile, KeyFile: keyFile}, config.XSpaces)

	settingstest.UnsetEnv(t, "X_SPACES_KEY_FILE")

	_, err = LoadRuntime()
	require.ErrorContains(t, err, "X_SPACES_CONFIG_FILE requires X_SPACES_KEY_FILE")
}
