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
	birthdayStreamRunnerEnabledEnv,
	"BIRTHDAY_STREAM_POLL_INTERVAL_MS",
	"BIRTHDAY_STREAM_SESSION_FRESHNESS_MS",
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

// 키가 없을 때만 기본값을 쓴다.
func TestHourOfDayEnvUsesDefaultWhenUnset(t *testing.T) {
	settingstest.UnsetEnv(t, "TEST_WORKER_CONFIG_HOUR")

	hour, err := hourOfDayEnv("TEST_WORKER_CONFIG_HOUR", 9)
	require.NoError(t, err)
	assert.Equal(t, 9, hour)
}

// 잘못된 값과 빈 값은 기본값으로 바뀌지 않고 key를 담은 오류가 된다.
func TestHourOfDayEnvRejectsInvalidValues(t *testing.T) {
	for _, value := range []string{"", "  ", "noon", "-1", "24", "99"} {
		t.Setenv("TEST_WORKER_CONFIG_HOUR", value)

		_, err := hourOfDayEnv("TEST_WORKER_CONFIG_HOUR", 0)
		require.Error(t, err, value)
		assert.Contains(t, err.Error(), "TEST_WORKER_CONFIG_HOUR", value)
	}
}

// runner 스위치의 잘못된 값은 "꺼짐"으로 읽히지 않고 기동 오류로 드러난다.
func TestLoadRuntimeRejectsInvalidBackgroundRunnerSwitch(t *testing.T) {
	for _, key := range []string{celebrationRunnerEnabledEnv, birthdayStreamRunnerEnabledEnv} {
		t.Run(key, func(t *testing.T) {
			setBackgroundRunnerRuntimeEnv(t)
			t.Setenv(key, "maybe")

			if _, err := LoadRuntime(); err == nil || !strings.Contains(err.Error(), key) {
				t.Fatalf("LoadRuntime() error = %v, want invalid %s rejection", err, key)
			}
		})
	}
}

// 켜진 runner의 잘못된 설정 값은 기본값으로 바뀌지 않고 기동 오류가 되고, 꺼진 runner의 같은 키는 읽지 않는다.
func TestLoadRuntimeValidatesBackgroundRunnerConfigOnlyWhenEnabled(t *testing.T) {
	for _, tc := range []struct {
		switchKey, key, value string
	}{
		{switchKey: celebrationRunnerEnabledEnv, key: "CELEBRATION_CHECK_HOUR_KST", value: "noon"},
		{switchKey: celebrationRunnerEnabledEnv, key: "CELEBRATION_CHECK_HOUR_KST", value: "24"},
		{switchKey: celebrationRunnerEnabledEnv, key: "CELEBRATION_RUN_INTERVAL_MS", value: "0"},
		{switchKey: celebrationRunnerEnabledEnv, key: "CELEBRATION_RUN_INTERVAL_MS", value: " "},
		{switchKey: birthdayStreamRunnerEnabledEnv, key: "BIRTHDAY_STREAM_POLL_INTERVAL_MS", value: "30m"},
		{switchKey: birthdayStreamRunnerEnabledEnv, key: "BIRTHDAY_STREAM_SESSION_FRESHNESS_MS", value: "-1"},
	} {
		t.Run(tc.key+"="+tc.value, func(t *testing.T) {
			setBackgroundRunnerRuntimeEnv(t)
			t.Setenv(tc.key, tc.value)

			t.Setenv(tc.switchKey, "false")

			config, err := LoadRuntime()
			require.NoError(t, err, "disabled runner must not read %s", tc.key)
			assert.Equal(t, CelebrationRunnerConfig{}, config.Celebration)
			assert.Equal(t, BirthdayStreamRunnerConfig{}, config.BirthdayStream)

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
		t.Setenv(birthdayStreamRunnerEnabledEnv, "true")

		config, err := LoadRuntime()
		require.NoError(t, err)
		assert.Equal(t, CelebrationRunnerConfig{Enabled: true, CheckHourKST: 0, RunInterval: time.Hour}, config.Celebration)
		assert.Equal(t, BirthdayStreamRunnerConfig{Enabled: true, RunInterval: 30 * time.Minute, SessionFreshness: 30 * time.Minute}, config.BirthdayStream)
	})

	t.Run("boundaries", func(t *testing.T) {
		setBackgroundRunnerRuntimeEnv(t)
		t.Setenv(celebrationRunnerEnabledEnv, "true")
		t.Setenv("CELEBRATION_CHECK_HOUR_KST", "23")
		t.Setenv("CELEBRATION_RUN_INTERVAL_MS", "1")
		t.Setenv(birthdayStreamRunnerEnabledEnv, "true")
		t.Setenv("BIRTHDAY_STREAM_POLL_INTERVAL_MS", "1")
		t.Setenv("BIRTHDAY_STREAM_SESSION_FRESHNESS_MS", "1")

		config, err := LoadRuntime()
		require.NoError(t, err)
		assert.Equal(t, CelebrationRunnerConfig{Enabled: true, CheckHourKST: 23, RunInterval: time.Millisecond}, config.Celebration)
		assert.Equal(t, BirthdayStreamRunnerConfig{Enabled: true, RunInterval: time.Millisecond, SessionFreshness: time.Millisecond}, config.BirthdayStream)
	})
}

// 수집 설정 파일이 있으면 세션 키 파일도 있어야 한다. 키만 있는 것은 수집이 꺼진 상태다.
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
