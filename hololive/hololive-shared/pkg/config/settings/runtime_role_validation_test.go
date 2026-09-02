package settings

import (
	"strings"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/config/settings/internal/load"
	"github.com/kapu/hololive-shared/pkg/config/settings/internal/settingstest"
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
		Webhook: WebhookConfig{RequireHMAC: true},
		Holodex: HolodexConfig{
			APIKey:  "x",
			Timeout: DefaultHolodexOperationalConfig().Timeout,
			LiveStatusFallback: HolodexLiveStatusFallbackConfig{
				MaxPerCycle:     1,
				WallClockBudget: time.Second,
			},
		},
		Postgres: PostgresConfig{SSLMode: load.PostgresSSLModeVerifyFull},
		Scraper: ScraperConfig{
			FetcherEngine: ScraperFetcherEngineNetHTTP,
			Backfill:      ScraperBackfillConfig{TargetGroup: "notification"},
		},
		OfficialSchedule:     DefaultOfficialScheduleConfig(),
		MaxResponseBodyBytes: DefaultMaxResponseBodyBytes,
		Environment:          load.EnvironmentProduction,
		APIWorkerProfile:     apiWorkerProfileFixture(t),
		AlarmWorkerProfile:   alarmWorkerProfileFixture(t),
	}
}

func TestValidateBotRuntimeRejectsNotificationEgressOwner(t *testing.T) {
	clearRuntimeRoleEnv(t)
	t.Setenv(load.NotificationEgressRoleEnv, load.NotificationEgressRoleOwner)

	err := validRuntimeRoleConfig(t).ValidateBotRuntime()
	if err == nil || !strings.Contains(err.Error(), "must not own proactive notification egress") {
		t.Fatalf("ValidateBotRuntime() error = %v, want proactive egress ownership rejection", err)
	}
}

func TestValidateBotRuntimeRejectsMixedCaseNotificationEgressOwner(t *testing.T) {
	clearRuntimeRoleEnv(t)
	t.Setenv(load.NotificationEgressRoleEnv, "Owner")

	err := validRuntimeRoleConfig(t).ValidateBotRuntime()
	if err == nil || err.Error() != "validate no notification egress ownership: bot must not own proactive notification egress; NOTIFICATION_EGRESS_ROLE=owner is reserved for alarm-worker" {
		t.Fatalf("ValidateBotRuntime() error = %v, want proactive egress ownership rejection", err)
	}
}
