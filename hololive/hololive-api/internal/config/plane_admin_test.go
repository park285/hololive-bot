package config

import (
	"strings"
	"testing"

	"github.com/kapu/hololive-api/internal/config/configtest"
	"github.com/kapu/hololive-shared/pkg/config/settingstest"
)

func setAdminPlaneEnv(t *testing.T) {
	t.Helper()
	settingstest.ClearRuntimeRoleEnv(t)
	t.Setenv("HOLODEX_API_KEY", "test-key")
	t.Setenv("KAKAO_ROOMS", "test-room")
	t.Setenv("API_SECRET_KEY", "test-api-key")
	t.Setenv("HOLOLIVE_HTTP_TRANSPORTS", "h3")
	t.Setenv("HOLOLIVE_H3_CERT_FILE", settingstest.HololiveH3CertPath)
	t.Setenv("HOLOLIVE_H3_KEY_FILE", settingstest.HololiveH3KeyPath)
	t.Setenv("SERVER_PORT", "30006")
	t.Setenv(settingstest.IrisWebhookTokenEnv, "")
	t.Setenv(settingstest.IrisBotTokenEnv, "")
	t.Setenv("IRIS_BASE_URL", "")
	t.Setenv("IRIS_BASE_URL_FILE", "")
}

func loadAdminPlaneRuntime(t *testing.T) error {
	t.Helper()

	if err := loadProcessEnv(); err != nil {
		return err
	}

	config, err := loadAdminPlaneConfig()
	if err != nil {
		return err
	}

	return config.Validate()
}

func TestAdminPlaneBootsWithoutIrisEgressTokens(t *testing.T) {
	setAdminPlaneEnv(t)

	if err := loadAdminPlaneRuntime(t); err != nil {
		t.Fatalf("admin plane load error = %v", err)
	}

	// 같은 env에서 bot plane은 Iris egress 입력을 요구한다.
	server := configtest.NewIrisRuntimeDiagnosticsServer(t, configtest.WorkerProfileDiagnosticsJSON())
	t.Setenv("IRIS_BASE_URL", server.URL)
	t.Setenv("IRIS_BASE_URL_ALLOWED_HOSTS", configtest.URLHostname(t, server.URL))
	t.Setenv("IRIS_TRANSPORT", "http1")
	t.Setenv(settingstest.IrisBotTokenEnv, "test-bot-token")
	settingstest.UseProfileFixture(t, configtest.APIProfileFixture(t))

	if _, err := LoadBotPlaneRuntime(); err == nil || !strings.Contains(err.Error(), "IRIS_WEBHOOK_TOKEN is required") {
		t.Fatalf("LoadBotPlaneRuntime() error = %v, want IRIS_WEBHOOK_TOKEN is required", err)
	}
}

func TestAdminPlaneIgnoresAccidentalIrisEgressInputs(t *testing.T) {
	setAdminPlaneEnv(t)
	settingstest.UnsetEnv(t, "STACK_WORKER_PROFILE_FILE")
	t.Setenv(settingstest.IrisBotTokenEnv, "accidental-egress-token")
	t.Setenv("IRIS_BASE_URL", "http://iris.invalid")

	if err := loadAdminPlaneRuntime(t); err != nil {
		t.Fatalf("admin plane load error = %v, want nil without Iris worker profile fetch", err)
	}
}

func TestPlaneLoadersIgnoreInvalidYouTubeCollectorEnv(t *testing.T) {
	t.Run("admin", func(t *testing.T) {
		setAdminPlaneEnv(t)
		t.Setenv("YOUTUBE_COLLECTOR_INSTANCE_ID", "INVALID")

		if err := loadAdminPlaneRuntime(t); err != nil {
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

// admin plane 로더가 X_SPACES_KEY_FILE을 읽어 X 세션 저장소 조립에 넘긴다. 값이 없으면 빈 경로(비활성)다.
func TestAdminPlaneLoadsXSpacesKeyFile(t *testing.T) {
	setAdminPlaneEnv(t)
	settingstest.UnsetEnv(t, "X_SPACES_KEY_FILE")

	config, err := loadAdminPlaneConfig()
	if err != nil {
		t.Fatalf("loadAdminPlaneConfig() error = %v", err)
	}

	if config.XSpacesKeyFile != "" {
		t.Fatalf("XSpacesKeyFile = %q, want empty when X_SPACES_KEY_FILE is unset", config.XSpacesKeyFile)
	}

	t.Setenv("X_SPACES_KEY_FILE", "/run/hololive-bot/x-spaces/key")

	config, err = loadAdminPlaneConfig()
	if err != nil {
		t.Fatalf("loadAdminPlaneConfig() error = %v", err)
	}

	if config.XSpacesKeyFile != "/run/hololive-bot/x-spaces/key" {
		t.Fatalf("XSpacesKeyFile = %q, want X_SPACES_KEY_FILE value", config.XSpacesKeyFile)
	}
}

// admin plane의 PHOTO_SYNC_ENABLED는 미설정이면 켜지고, 잘못된 값은 기본값으로 바꾸지 않고 오류로 돌려준다.
func TestLoadIngestionConfigPhotoSyncEnv(t *testing.T) {
	settingstest.UnsetEnv(t, "PHOTO_SYNC_ENABLED")

	config, err := loadIngestionConfig()
	if err != nil || !config.PhotoSyncEnabled {
		t.Fatalf("loadIngestionConfig() unset = (%+v, %v), want photo sync enabled", config, err)
	}

	t.Setenv("PHOTO_SYNC_ENABLED", "false")

	config, err = loadIngestionConfig()
	if err != nil || config.PhotoSyncEnabled {
		t.Fatalf("loadIngestionConfig() false = (%+v, %v), want photo sync disabled", config, err)
	}

	t.Setenv("PHOTO_SYNC_ENABLED", "maybe")

	if _, err := loadIngestionConfig(); err == nil || !strings.Contains(err.Error(), "PHOTO_SYNC_ENABLED") {
		t.Fatalf("loadIngestionConfig() invalid error = %v, want PHOTO_SYNC_ENABLED error", err)
	}
}
