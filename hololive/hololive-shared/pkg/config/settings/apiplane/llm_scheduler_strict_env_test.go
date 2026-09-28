package apiplane

import (
	"strings"
	"testing"

	"github.com/kapu/hololive-shared/pkg/config/settings/internal/settingstest"
)

// llm plane 설정도 숫자·bool env의 잘못된 값을 기본값으로 바꾸지 않고 기동 실패로 드러낸다(stack audit B4).
func TestLoadLLMSchedulerRuntimeRejectsInvalidEnvValues(t *testing.T) {
	for _, tc := range []struct {
		key   string
		value string
	}{
		{key: "LLM_SCHEDULER_PORT", value: "abc"},
		{key: "GEMINI_ENABLED", value: "maybe"},
		{key: "CLIPROXY_ENABLED", value: "maybe"},
		{key: "CACHE_PORT", value: "invalid"},
		{key: "MAJOREVENT_CONSENSUS_CONFIDENCE", value: "high"},
		{key: "BOT_SEE_MORE_FOLD", value: "maybe"},
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
