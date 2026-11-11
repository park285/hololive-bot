package config

import (
	"strings"
	"testing"
)

const invalidBoolEnvValue = "maybe"

// llm plane 설정도 숫자·bool env의 잘못된 값을 기본값으로 바꾸지 않고 기동 실패로 드러낸다.
func TestBuildLLMSchedulerConfigRejectsInvalidEnvValues(t *testing.T) {
	for _, tc := range []struct {
		key   string
		value string
	}{
		{key: "GEMINI_ENABLED", value: invalidBoolEnvValue},
		{key: "CLIPROXY_ENABLED", value: invalidBoolEnvValue},
		{key: "MAJOREVENT_CONSENSUS_CONFIDENCE", value: "high"},
		{key: "MEMBER_NEWS_TEMPERATURE", value: "warm"},
		{key: "EXA_ENABLED", value: invalidBoolEnvValue},
		{key: "BOT_SEE_MORE_FOLD", value: invalidBoolEnvValue},
	} {
		t.Run(tc.key+"="+tc.value, func(t *testing.T) {
			t.Setenv(tc.key, tc.value)

			_, err := buildLLMSchedulerConfig()
			if err == nil {
				t.Fatalf("buildLLMSchedulerConfig() accepted invalid %s=%q; want a startup error", tc.key, tc.value)
			}

			if !strings.Contains(err.Error(), tc.key) {
				t.Fatalf("buildLLMSchedulerConfig() error = %v, want it to name %s", err, tc.key)
			}
		})
	}
}
