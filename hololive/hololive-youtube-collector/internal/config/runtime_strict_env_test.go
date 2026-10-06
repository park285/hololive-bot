package config

import (
	"strings"
	"testing"
)

// collector runtime도 숫자·bool env의 잘못된 값을 기본값으로 바꾸지 않고 기동 실패로 드러낸다(stack audit B4).
func TestLoadRuntimeRejectsInvalidEnvValues(t *testing.T) {
	for _, tc := range []struct {
		key   string
		value string
	}{
		{key: "HOLODEX_TIMEOUT_SECONDS", value: "25s"},
		{key: "OFFICIAL_SCHEDULE_TIMEOUT_SECONDS", value: "15s"},
		{key: "SERVER_PORT", value: "invalid"},
		{key: "LOG_MAX_BACKUPS", value: "five"},
		{key: "LOG_COMPRESS", value: "maybe"},
		{key: "POSTGRES_POOL_MAX_CONNS", value: "eight"},
	} {
		t.Run(tc.key+"="+tc.value, func(t *testing.T) {
			setYouTubeCollectorRuntimeLoadEnv(t)
			t.Setenv(tc.key, tc.value)

			_, err := LoadRuntime()
			if err == nil {
				t.Fatalf("LoadRuntime() accepted invalid %s=%q; want a startup error", tc.key, tc.value)
			}

			if !strings.Contains(err.Error(), tc.key) {
				t.Fatalf("LoadRuntime() error = %v, want it to name %s", err, tc.key)
			}
		})
	}
}
