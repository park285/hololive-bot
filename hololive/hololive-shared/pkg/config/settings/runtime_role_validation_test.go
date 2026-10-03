package settings

import (
	"strings"
	"testing"

	"github.com/kapu/hololive-shared/pkg/config/runtimepolicy"
	"github.com/kapu/hololive-shared/pkg/config/settingstest"
)

func clearRuntimeRoleEnv(t *testing.T) {
	t.Helper()
	settingstest.ClearRuntimeRoleEnv(t)
}

func validRuntimeRoleConfig(t *testing.T) *Config {
	t.Helper()

	return &Config{
		Server: ServerConfig{
			Port:           30001,
			APIKey:         "x",
			HTTPTransports: []string{"h3"},
			H3Addr:         ":30001",
			H3CertFile:     "/run/hololive-bot/certs/hololive-h3.crt",
			H3KeyFile:      hololiveH3KeyPath,
		},
		Kakao: KakaoConfig{Rooms: []string{"room"}},
		Iris: IrisConfig{
			BaseURL:      "https://iris.example.invalid",
			WebhookToken: "x",
			BotToken:     "x",
		},
		Holodex: HolodexConfig{
			APIKey:  "x",
			Timeout: DefaultHolodexOperationalConfig().Timeout,
		},
		Postgres:             PostgresConfig{SSLMode: runtimepolicy.PostgresSSLModeVerifyFull},
		OfficialSchedule:     DefaultOfficialScheduleConfig(),
		MaxResponseBodyBytes: DefaultMaxResponseBodyBytes,
		Environment:          runtimepolicy.EnvironmentProduction,
		APIWorkerProfile:     apiWorkerProfileFixture(t),
		AlarmWorkerProfile:   alarmWorkerProfileFixture(t),
	}
}

func TestValidateBotRuntimeRejectsNotificationEgressOwner(t *testing.T) {
	clearRuntimeRoleEnv(t)
	t.Setenv(runtimepolicy.NotificationEgressRoleEnv, runtimepolicy.NotificationEgressRoleOwner)

	err := validRuntimeRoleConfig(t).ValidateBotRuntime()
	if err == nil || !strings.Contains(err.Error(), "must not own proactive notification egress") {
		t.Fatalf("ValidateBotRuntime() error = %v, want proactive egress ownership rejection", err)
	}
}

func TestValidateBotRuntimeRejectsMixedCaseNotificationEgressOwner(t *testing.T) {
	clearRuntimeRoleEnv(t)
	t.Setenv(runtimepolicy.NotificationEgressRoleEnv, "Owner")

	err := validRuntimeRoleConfig(t).ValidateBotRuntime()
	if err == nil || err.Error() != "validate no notification egress ownership: bot must not own proactive notification egress; NOTIFICATION_EGRESS_ROLE=owner is reserved for alarm-worker" {
		t.Fatalf("ValidateBotRuntime() error = %v, want proactive egress ownership rejection", err)
	}
}
