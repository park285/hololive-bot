package config

import (
	"strings"
	"testing"

	"github.com/kapu/hololive-shared/pkg/config/settingstest"
)

const invalidBoolEnvValue = "maybe"

func TestLoadLLMSchedulerRuntimeRejectsInvalidEnvValues(t *testing.T) {
	for _, tc := range []struct {
		key   string
		value string
	}{
		{key: "LLM_SCHEDULER_PORT", value: "abc"},
		{key: "GEMINI_ENABLED", value: invalidBoolEnvValue},
		{key: "CLIPROXY_ENABLED", value: invalidBoolEnvValue},
		{key: "CACHE_PORT", value: "invalid"},
		{key: "MAJOREVENT_CONSENSUS_CONFIDENCE", value: "high"},
		{key: "MEMBER_NEWS_TEMPERATURE", value: "warm"},
		{key: "EXA_ENABLED", value: invalidBoolEnvValue},
		{key: "BOT_SEE_MORE_FOLD", value: invalidBoolEnvValue},
	} {
		t.Run(tc.key+"="+tc.value, func(t *testing.T) {
			settingstest.ClearIrisAndRoomEnv(t)
			settingstest.SetRuntimeH3ServerEnv(t)
			t.Setenv("API_SECRET_KEY", "dummy-secret")
			t.Setenv(tc.key, tc.value)

			_, err := LoadLLMSchedulerRuntime()
			if err == nil {
				t.Fatalf("LoadLLMSchedulerRuntime() accepted invalid %s=%q; want a startup error", tc.key, tc.value)
			}

			if !strings.Contains(err.Error(), tc.key) {
				t.Fatalf("LoadLLMSchedulerRuntime() error = %v, want it to name %s", err, tc.key)
			}
		})
	}
}
