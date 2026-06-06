package settings

import (
	"strings"
	"testing"
)

func clearIrisAndRoomEnv(t *testing.T) {
	t.Helper()

	for _, key := range []string{
		"IRIS_WEBHOOK_TOKEN",
		"IRIS_BOT_TOKEN",
		"IRIS_BASE_URL",
		"IRIS_BASE_URL_FILE",
		"KAKAO_ROOMS",
	} {
		t.Setenv(key, "")
	}
}

func TestLoadLLMSchedulerRuntimeAllowsMissingIrisInputs(t *testing.T) {
	clearIrisAndRoomEnv(t)
	t.Setenv("API_SECRET_KEY", "dummy-secret")

	cfg, err := LoadLLMSchedulerRuntime()
	if err != nil {
		t.Fatalf("LoadLLMSchedulerRuntime() error = %v", err)
	}
	if cfg.Server.Port != 30003 {
		t.Fatalf("Server.Port = %d, want 30003", cfg.Server.Port)
	}
}

func TestLoadLLMSchedulerStillRequiresIrisTokens(t *testing.T) {
	clearIrisAndRoomEnv(t)
	t.Setenv("API_SECRET_KEY", "dummy-secret")

	_, err := LoadLLMScheduler()
	if err == nil || !strings.Contains(err.Error(), "IRIS_WEBHOOK_TOKEN") {
		t.Fatalf("LoadLLMScheduler() error = %v, want IRIS_WEBHOOK_TOKEN requirement", err)
	}
}

func TestLoadYouTubeProducerRuntimeAllowsMissingIrisAndRooms(t *testing.T) {
	clearIrisAndRoomEnv(t)
	t.Setenv("API_SECRET_KEY", "dummy-secret")
	t.Setenv("HOLOLIVE_HTTP_TRANSPORTS", "h2c")
	t.Setenv("HOLODEX_API_KEY", "dummy-holodex")
	t.Setenv("YOUTUBE_API_KEY", "dummy-youtube-key")

	cfg, err := LoadYouTubeProducerRuntime()
	if err != nil {
		t.Fatalf("LoadYouTubeProducerRuntime() error = %v", err)
	}
	if cfg.Holodex.APIKey != "dummy-holodex" {
		t.Fatalf("Holodex.APIKey = %q, want dummy-holodex", cfg.Holodex.APIKey)
	}
}

func TestLoadYouTubeProducerRuntimeRequiresRealYouTubeKey(t *testing.T) {
	clearIrisAndRoomEnv(t)
	t.Setenv("API_SECRET_KEY", "dummy-secret")
	t.Setenv("HOLOLIVE_HTTP_TRANSPORTS", "h2c")
	t.Setenv("HOLODEX_API_KEY", "dummy-holodex")
	t.Setenv("YOUTUBE_API_KEY", "changeme")

	_, err := LoadYouTubeProducerRuntime()
	if err == nil || !strings.Contains(err.Error(), "YOUTUBE_API_KEY") {
		t.Fatalf("LoadYouTubeProducerRuntime() error = %v, want YOUTUBE_API_KEY requirement", err)
	}
}

func TestLoadYouTubeProducerRuntimeRequiresHolodexKey(t *testing.T) {
	clearIrisAndRoomEnv(t)
	t.Setenv("API_SECRET_KEY", "dummy-secret")
	t.Setenv("HOLOLIVE_HTTP_TRANSPORTS", "h2c")
	t.Setenv("HOLODEX_API_KEY", "")
	t.Setenv("HOLODEX_API_KEY_1", "")
	t.Setenv("YOUTUBE_API_KEY", "dummy-youtube-key")

	_, err := LoadYouTubeProducerRuntime()
	if err == nil || !strings.Contains(err.Error(), "HOLODEX_API_KEY") {
		t.Fatalf("LoadYouTubeProducerRuntime() error = %v, want HOLODEX_API_KEY requirement", err)
	}
}
