package config

import (
	"strings"
	"testing"

	"github.com/kapu/hololive-shared/pkg/config/settingstest"
)

func setAdminPlaneEnv(t *testing.T) {
	t.Helper()
	settingstest.ClearRuntimeRoleEnv(t)
	settingstest.ClearTracingEnv(t)
	t.Setenv("HOLODEX_API_KEY", "test-key")
	t.Setenv("KAKAO_ROOMS", "test-room")
	t.Setenv("API_SECRET_KEY", "test-api-key")
	t.Setenv("HOLOLIVE_HTTP_TRANSPORTS", "h3")
	t.Setenv("HOLOLIVE_H3_CERT_FILE", settingstest.HololiveH3CertPath)
	t.Setenv("HOLOLIVE_H3_KEY_FILE", settingstest.HololiveH3KeyPath)
	t.Setenv("SERVER_PORT", "30006")
	t.Setenv("CORS_ALLOWED_ORIGINS", "https://admin.example.com")
	t.Setenv(settingstest.IrisWebhookTokenEnv, "")
	t.Setenv(settingstest.IrisBotTokenEnv, "")
	t.Setenv("IRIS_BASE_URL", "")
	t.Setenv("IRIS_BASE_URL_FILE", "")
	t.Setenv("YOUTUBE_API_KEY", "")
}

// loadAdminPlaneRuntime은 LoadRuntime이 admin plane에 적용하는 공통 env 단계와 역할 검증을 그대로 거친다.
func loadAdminPlaneRuntime(t *testing.T) (*AdminPlaneConfig, error) {
	t.Helper()

	if err := loadProcessEnv(); err != nil {
		return nil, err
	}

	config, err := loadAdminPlaneConfig()
	if err != nil {
		return nil, err
	}

	if err := config.Validate(); err != nil {
		return nil, err
	}

	return config, nil
}

func TestAdminPlaneBootsWithoutIrisEgressTokens(t *testing.T) {
	setAdminPlaneEnv(t)

	if _, err := loadAdminPlaneRuntime(t); err != nil {
		t.Fatalf("admin plane load error = %v", err)
	}

	// 같은 env에서 bot plane은 Iris egress 입력을 요구한다.
	server := settingstest.NewIrisRuntimeDiagnosticsServer(t, settingstest.WorkerProfileDiagnosticsJSON())
	t.Setenv("IRIS_BASE_URL", server.URL)
	t.Setenv("IRIS_BASE_URL_ALLOWED_HOSTS", settingstest.URLHostname(t, server.URL))
	t.Setenv("IRIS_TRANSPORT", "http1")
	t.Setenv(settingstest.IrisBotTokenEnv, "test-bot-token")
	settingstest.UseProfileFixture(t, "stack-worker-profile-api.json")

	if _, err := LoadBotPlaneRuntime(); err == nil || !strings.Contains(err.Error(), "IRIS_WEBHOOK_TOKEN is required") {
		t.Fatalf("LoadBotPlaneRuntime() error = %v, want IRIS_WEBHOOK_TOKEN is required", err)
	}
}

// admin plane은 nonEgress라 Iris 토큰이 실수로 주입돼도 worker profile을 읽거나 Iris에 접속하지 않는다.
func TestAdminPlaneIgnoresAccidentalIrisEgressInputs(t *testing.T) {
	setAdminPlaneEnv(t)
	settingstest.UnsetEnv(t, "STACK_WORKER_PROFILE_FILE")
	t.Setenv(settingstest.IrisBotTokenEnv, "accidental-egress-token")
	t.Setenv("IRIS_BASE_URL", "http://iris.invalid")

	if _, err := loadAdminPlaneRuntime(t); err != nil {
		t.Fatalf("admin plane load error = %v, want nil without Iris worker profile fetch", err)
	}
}

func TestAdminPlaneDefaultEnforcesCORSOrigins(t *testing.T) {
	setAdminPlaneEnv(t)
	t.Setenv("CORS_ALLOWED_ORIGINS", "")

	_, err := loadAdminPlaneRuntime(t)
	if err == nil || !strings.Contains(err.Error(), "CORS_ALLOWED_ORIGINS is required in production when CORS_ENFORCE=true") {
		t.Fatalf("admin plane load error = %v, want missing CORS_ALLOWED_ORIGINS error", err)
	}
}

func TestAdminPlaneRequiresHolodexKeyAndRooms(t *testing.T) {
	for key, want := range map[string]string{
		"HOLODEX_API_KEY": "HOLODEX_API_KEY is required",
		"KAKAO_ROOMS":     "KAKAO_ROOMS is required",
	} {
		t.Run(key, func(t *testing.T) {
			setAdminPlaneEnv(t)
			t.Setenv(key, "")

			_, err := loadAdminPlaneRuntime(t)
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("admin plane load error = %v, want %q", err, want)
			}
		})
	}
}

func TestAdminPlaneReadsCanonicalLLMSchedulerHealthURL(t *testing.T) {
	setAdminPlaneEnv(t)
	t.Setenv("SERVICES_LLM_SCHEDULER_HEALTH_URL", " https://llm.internal/health ")

	config, err := loadAdminPlaneRuntime(t)
	if err != nil {
		t.Fatalf("admin plane load error = %v", err)
	}

	if config.Services.LLMSchedulerHealthURL != "https://llm.internal/health" {
		t.Fatalf("Services.LLMSchedulerHealthURL = %q, want canonical value", config.Services.LLMSchedulerHealthURL)
	}
}

// bot·admin plane 로더는 youtube-collector 전용 env를 읽지 않는다.
func TestPlaneLoadersIgnoreInvalidYouTubeCollectorEnv(t *testing.T) {
	t.Run("admin", func(t *testing.T) {
		setAdminPlaneEnv(t)
		t.Setenv("YOUTUBE_COLLECTOR_INSTANCE_ID", "INVALID")

		if _, err := loadAdminPlaneRuntime(t); err != nil {
			t.Fatalf("admin plane load error = %v, want success when collector env is invalid", err)
		}
	})

	t.Run("bot", func(t *testing.T) {
		setBotPlaneEnv(t)
		t.Setenv("YOUTUBE_COLLECTOR_INSTANCE_ID", "INVALID")

		if _, err := LoadBotPlaneRuntime(); err != nil {
			t.Fatalf("LoadBotPlaneRuntime() error = %v, want success when collector env is invalid", err)
		}
	})
}
