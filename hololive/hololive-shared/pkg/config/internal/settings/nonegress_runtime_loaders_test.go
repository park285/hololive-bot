package settings

import (
	"strings"
	"testing"
)

func clearIrisAndRoomEnv(t *testing.T) {
	t.Helper()
	clearRuntimeRoleEnv(t)

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

func setRuntimeH3ServerEnv(t *testing.T) {
	t.Helper()

	t.Setenv("HOLOLIVE_HTTP_TRANSPORTS", "h3")
	t.Setenv("HOLOLIVE_H3_CERT_FILE", "/run/hololive-bot/certs/hololive-h3.crt")
	t.Setenv("HOLOLIVE_H3_KEY_FILE", "/run/hololive-bot/certs/hololive-h3.key")
}

func TestLoadLLMSchedulerRuntimeAllowsMissingIrisInputs(t *testing.T) {
	clearIrisAndRoomEnv(t)
	setRuntimeH3ServerEnv(t)
	t.Setenv("API_SECRET_KEY", "dummy-secret")

	cfg, err := LoadLLMSchedulerRuntime()
	if err != nil {
		t.Fatalf("LoadLLMSchedulerRuntime() error = %v", err)
	}
	if cfg.Server.Port != 30003 {
		t.Fatalf("Server.Port = %d, want 30003", cfg.Server.Port)
	}
	if !cfg.Server.TransportEnabled("h3") {
		t.Fatal("Server.TransportEnabled(h3) = false, want true")
	}
}

func TestLoadLLMSchedulerStillRequiresIrisTokens(t *testing.T) {
	clearIrisAndRoomEnv(t)
	setRuntimeH3ServerEnv(t)
	t.Setenv("API_SECRET_KEY", "dummy-secret")

	_, err := LoadLLMScheduler()
	if err == nil || !strings.Contains(err.Error(), "IRIS_WEBHOOK_TOKEN") {
		t.Fatalf("LoadLLMScheduler() error = %v, want IRIS_WEBHOOK_TOKEN requirement", err)
	}
}

func TestLoadYouTubeProducerRuntimeAllowsMissingIrisAndRooms(t *testing.T) {
	clearIrisAndRoomEnv(t)
	t.Setenv("API_SECRET_KEY", "dummy-secret")
	setRuntimeH3ServerEnv(t)
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

func TestNonEgressConfigLoadersSkipWorkerProfileFetchWithAccidentalIrisToken(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*testing.T)
		load  func() (*Config, error)
	}{
		{
			name: "admin api",
			setup: func(t *testing.T) {
				t.Helper()
				setAdminAPIRuntimeEnv(t)
			},
			load: LoadAdminAPIRuntime,
		},
		{
			name: "youtube producer",
			setup: func(t *testing.T) {
				t.Helper()
				clearIrisAndRoomEnv(t)
				t.Setenv("API_SECRET_KEY", "dummy-secret")
				setRuntimeH3ServerEnv(t)
				t.Setenv("HOLODEX_API_KEY", "dummy-holodex")
				t.Setenv("YOUTUBE_API_KEY", "dummy-youtube-key")
			},
			load: LoadYouTubeProducerRuntime,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.setup(t)
			t.Setenv("IRIS_BOT_TOKEN", "accidental-egress-token")
			t.Setenv("IRIS_BASE_URL", "http://iris.invalid")

			cfg, err := tt.load()
			if err != nil {
				t.Fatalf("%s load error = %v, want nil without Iris worker profile fetch", tt.name, err)
			}
			if cfg.WorkerPool.Workers != 10 || cfg.WorkerPool.QueueSize != 100 {
				t.Fatalf("%s WorkerPool = %#v, want legacy worker profile defaults", tt.name, cfg.WorkerPool)
			}
			if cfg.Webhook.WorkerCount != 16 || cfg.Webhook.QueueSize != 1000 {
				t.Fatalf("%s Webhook = %#v, want legacy worker profile defaults", tt.name, cfg.Webhook)
			}
		})
	}
}

func TestLoadYouTubeProducerRuntimeRequiresHolodexKey(t *testing.T) {
	clearIrisAndRoomEnv(t)
	t.Setenv("API_SECRET_KEY", "dummy-secret")
	setRuntimeH3ServerEnv(t)
	t.Setenv("HOLODEX_API_KEY", "")
	t.Setenv("HOLODEX_API_KEY_1", "")
	t.Setenv("YOUTUBE_API_KEY", "dummy-youtube-key")

	_, err := LoadYouTubeProducerRuntime()
	if err == nil || !strings.Contains(err.Error(), "HOLODEX_API_KEY") {
		t.Fatalf("LoadYouTubeProducerRuntime() error = %v, want HOLODEX_API_KEY requirement", err)
	}
}
