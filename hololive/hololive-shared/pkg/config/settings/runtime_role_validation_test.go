package settings

import (
	"strings"
	"testing"
	"time"
)

func clearRuntimeRoleEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		notificationEgressRoleEnv,
		notificationSchedulerRoleEnv,
		deliveryDispatcherEnabledEnv,
		youTubeOutboxDispatcherEnabledEnv,
		alarmDispatchConsumerEnabledEnv,
		"MEMBER_NEWS_CLIPROXY_MODEL",
		"DB_SSLMODE",
		"DB_QUERY_EXEC_MODE",
		"OTEL_ENVIRONMENT",
	} {
		t.Setenv(key, "")
	}
}

func validRuntimeRoleConfig() *Config {
	return &Config{
		Server: ServerConfig{
			Port:           30001,
			APIKey:         "x",
			HTTPTransports: []string{"h3"},
			H3Addr:         ":30001",
			H3CertFile:     "/run/hololive-bot/certs/hololive-h3.crt",
			H3KeyFile:      "/run/hololive-bot/certs/hololive-h3.key",
		},
		Kakao: KakaoConfig{Rooms: []string{"room"}},
		Iris: IrisConfig{
			BaseURL:      "https://iris.example.invalid",
			WebhookToken: "x",
			BotToken:     "x",
		},
		Webhook: WebhookConfig{RequireHMAC: true},
		Holodex: HolodexConfig{
			APIKey: "x",
			LiveStatusFallback: HolodexLiveStatusFallbackConfig{
				MaxPerCycle:     1,
				WallClockBudget: time.Second,
			},
		},
		Postgres: PostgresConfig{SSLMode: "verify-full"},
		Scraper: ScraperConfig{
			FetcherEngine: ScraperFetcherEngineNetHTTP,
			Backfill:      ScraperBackfillConfig{TargetGroup: "notification"},
		},
		OfficialSchedule:     DefaultOfficialScheduleConfig(),
		MaxResponseBodyBytes: DefaultMaxResponseBodyBytes,
		Environment:          "production",
	}
}

func validLLMSchedulerRuntimeConfig() *LLMSchedulerConfig {
	return &LLMSchedulerConfig{
		Server: ServerConfig{
			Port:           30003,
			APIKey:         "x",
			HTTPTransports: []string{"h3"},
			H3Addr:         ":30003",
			H3CertFile:     "/run/hololive-bot/certs/hololive-h3.crt",
			H3KeyFile:      "/run/hololive-bot/certs/hololive-h3.key",
		},
		Postgres:    PostgresConfig{SSLMode: "verify-full"},
		Environment: "production",
	}
}

func TestValidateBotRuntimeRejectsNotificationEgressOwner(t *testing.T) {
	clearRuntimeRoleEnv(t)
	t.Setenv(notificationEgressRoleEnv, notificationEgressRoleOwner)

	err := validRuntimeRoleConfig().ValidateBotRuntime()
	if err == nil || !strings.Contains(err.Error(), "must not own proactive notification egress") {
		t.Fatalf("ValidateBotRuntime() error = %v, want proactive egress ownership rejection", err)
	}
}

func TestValidateBotRuntimeRejectsMixedCaseNotificationEgressOwner(t *testing.T) {
	clearRuntimeRoleEnv(t)
	t.Setenv(notificationEgressRoleEnv, "Owner")

	err := validRuntimeRoleConfig().ValidateBotRuntime()
	if err == nil || err.Error() != "bot must not own proactive notification egress; NOTIFICATION_EGRESS_ROLE=owner is reserved for alarm-worker" {
		t.Fatalf("ValidateBotRuntime() error = %v, want proactive egress ownership rejection", err)
	}
}

func TestValidateAdminAPIRuntimeRejectsDispatchers(t *testing.T) {
	clearRuntimeRoleEnv(t)
	t.Setenv(deliveryDispatcherEnabledEnv, "true")

	err := validRuntimeRoleConfig().ValidateAdminAPIRuntime()
	if err == nil || !strings.Contains(err.Error(), deliveryDispatcherEnabledEnv) {
		t.Fatalf("ValidateAdminAPIRuntime() error = %v, want delivery dispatcher rejection", err)
	}
}

func TestValidateYouTubeCollectorRuntimeRejectsYouTubeOutboxDispatcher(t *testing.T) {
	clearRuntimeRoleEnv(t)
	t.Setenv(youTubeOutboxDispatcherEnabledEnv, "true")

	cfg := validRuntimeRoleConfig()
	cfg.Postgres.User = postgresScraperRoleUser
	err := cfg.ValidateYouTubeCollectorRuntime()
	if err == nil || !strings.Contains(err.Error(), youTubeOutboxDispatcherEnabledEnv) {
		t.Fatalf("ValidateYouTubeCollectorRuntime() error = %v, want YouTube outbox dispatcher rejection", err)
	}
}

func TestValidateYouTubeCollectorRuntimeRequiresScraperPostgresUser(t *testing.T) {
	clearRuntimeRoleEnv(t)
	err := validRuntimeRoleConfig().ValidateYouTubeCollectorRuntime()
	if err == nil || !strings.Contains(err.Error(), "POSTGRES_USER=hololive_scraper") {
		t.Fatalf("ValidateYouTubeCollectorRuntime() error = %v, want scraper postgres user", err)
	}
}

func TestValidateYouTubeCollectorRuntimeRejectsActiveActive(t *testing.T) {
	clearRuntimeRoleEnv(t)
	cfg := validRuntimeRoleConfig()
	cfg.Postgres.User = postgresScraperRoleUser
	cfg.Scraper.ActiveActive.Enabled = true
	err := cfg.ValidateYouTubeCollectorRuntime()
	if err == nil || !strings.Contains(err.Error(), "YOUTUBE_PRODUCER_ACTIVE_ACTIVE_ENABLED") {
		t.Fatalf("ValidateYouTubeCollectorRuntime() error = %v, want active-active rejection", err)
	}
}

func TestValidateYouTubeCollectorRuntimeAllowsMissingHolodexAPIKey(t *testing.T) {
	clearRuntimeRoleEnv(t)
	cfg := validRuntimeRoleConfig()
	cfg.Postgres.User = postgresScraperRoleUser
	cfg.Holodex.APIKey = ""
	if err := cfg.ValidateYouTubeCollectorRuntime(); err != nil {
		t.Fatalf("ValidateYouTubeCollectorRuntime() error = %v, want nil without Holodex key", err)
	}
}

func TestValidateLLMSchedulerRuntimeRejectsSchedulerWorkerRole(t *testing.T) {
	clearRuntimeRoleEnv(t)
	t.Setenv(notificationSchedulerRoleEnv, notificationSchedulerRoleWorker)

	err := validLLMSchedulerRuntimeConfig().validateRuntime()
	if err == nil || !strings.Contains(err.Error(), "must not run the alarm scheduler role") {
		t.Fatalf("LLMSchedulerConfig.validateRuntime() error = %v, want scheduler role rejection", err)
	}
}

func TestValidateAlarmWorkerRuntimeProductionRequiresOwnerWorkerRoles(t *testing.T) {
	clearRuntimeRoleEnv(t)

	err := validRuntimeRoleConfig().ValidateAlarmWorkerRuntime()
	if err == nil || err.Error() != "alarm-worker production requires NOTIFICATION_EGRESS_ROLE=owner" {
		t.Fatalf("ValidateAlarmWorkerRuntime() error = %v, want owner role requirement", err)
	}

	clearRuntimeRoleEnv(t)
	t.Setenv(notificationEgressRoleEnv, notificationEgressRoleOwner)
	err = validRuntimeRoleConfig().ValidateAlarmWorkerRuntime()
	if err == nil || err.Error() != "alarm-worker production requires NOTIFICATION_SCHEDULER_ROLE=worker|off" {
		t.Fatalf("ValidateAlarmWorkerRuntime() error = %v, want scheduler role enumeration requirement", err)
	}
}

func TestValidateAlarmWorkerRuntimeProductionRejectsNonOwnerEgressRoles(t *testing.T) {
	for _, role := range []string{notificationEgressRoleProducer, notificationEgressRoleOff} {
		t.Run(role, func(t *testing.T) {
			clearRuntimeRoleEnv(t)
			t.Setenv(notificationEgressRoleEnv, role)
			t.Setenv(notificationSchedulerRoleEnv, notificationSchedulerRoleWorker)
			t.Setenv(deliveryDispatcherEnabledEnv, "true")
			t.Setenv(alarmDispatchConsumerEnabledEnv, "true")
			t.Setenv(youTubeOutboxDispatcherEnabledEnv, "true")

			err := validRuntimeRoleConfig().ValidateAlarmWorkerRuntime()
			if err == nil || err.Error() != "alarm-worker production requires NOTIFICATION_EGRESS_ROLE=owner" {
				t.Fatalf("ValidateAlarmWorkerRuntime() error = %v, want owner role requirement", err)
			}
		})
	}
}

func TestValidateAlarmWorkerRuntimeNonProductionSkipsOwnershipRequirements(t *testing.T) {
	clearRuntimeRoleEnv(t)

	cfg := validRuntimeRoleConfig()
	cfg.Environment = "staging"

	if err := cfg.ValidateAlarmWorkerRuntime(); err != nil {
		t.Fatalf("ValidateAlarmWorkerRuntime() error = %v, want nil", err)
	}
}

func TestValidateAlarmWorkerRuntimeProductionAcceptsSchedulerWorkerProfile(t *testing.T) {
	clearRuntimeRoleEnv(t)
	t.Setenv(notificationEgressRoleEnv, notificationEgressRoleOwner)
	t.Setenv(notificationSchedulerRoleEnv, notificationSchedulerRoleWorker)
	t.Setenv(deliveryDispatcherEnabledEnv, "true")
	t.Setenv(alarmDispatchConsumerEnabledEnv, "true")
	t.Setenv(youTubeOutboxDispatcherEnabledEnv, "true")

	if err := validRuntimeRoleConfig().ValidateAlarmWorkerRuntime(); err != nil {
		t.Fatalf("ValidateAlarmWorkerRuntime() error = %v, want nil", err)
	}
}

func TestValidateAlarmWorkerRuntimeProductionAcceptsEgressOnlyProfile(t *testing.T) {
	clearRuntimeRoleEnv(t)
	t.Setenv(notificationEgressRoleEnv, notificationEgressRoleOwner)
	t.Setenv(notificationSchedulerRoleEnv, notificationSchedulerRoleOff)
	t.Setenv(deliveryDispatcherEnabledEnv, "true")
	t.Setenv(alarmDispatchConsumerEnabledEnv, "true")
	t.Setenv(youTubeOutboxDispatcherEnabledEnv, "true")

	if err := validRuntimeRoleConfig().ValidateAlarmWorkerRuntime(); err != nil {
		t.Fatalf("ValidateAlarmWorkerRuntime() error = %v, want nil", err)
	}
}

func TestValidateAlarmWorkerRuntimeRejectsUnsupportedSchedulerRole(t *testing.T) {
	clearRuntimeRoleEnv(t)
	t.Setenv(notificationEgressRoleEnv, notificationEgressRoleOwner)
	t.Setenv(notificationSchedulerRoleEnv, "bot")

	err := validRuntimeRoleConfig().ValidateAlarmWorkerRuntime()
	if err == nil || err.Error() != "unsupported NOTIFICATION_SCHEDULER_ROLE=bot" {
		t.Fatalf("ValidateAlarmWorkerRuntime() error = %v, want unsupported scheduler role rejection", err)
	}
}

func TestValidateAlarmWorkerRuntimeProductionRejectsDisabledDispatchers(t *testing.T) {
	tests := []struct {
		name string
		key  string
	}{
		{name: "delivery dispatcher", key: deliveryDispatcherEnabledEnv},
		{name: "alarm dispatch consumer", key: alarmDispatchConsumerEnabledEnv},
		{name: "youtube outbox dispatcher", key: youTubeOutboxDispatcherEnabledEnv},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearRuntimeRoleEnv(t)
			t.Setenv(notificationEgressRoleEnv, notificationEgressRoleOwner)
			t.Setenv(notificationSchedulerRoleEnv, notificationSchedulerRoleWorker)
			t.Setenv(deliveryDispatcherEnabledEnv, "true")
			t.Setenv(alarmDispatchConsumerEnabledEnv, "true")
			t.Setenv(youTubeOutboxDispatcherEnabledEnv, "true")
			t.Setenv(tt.key, "false")

			err := validRuntimeRoleConfig().ValidateAlarmWorkerRuntime()
			if err == nil || !strings.Contains(err.Error(), "requires "+tt.key+"=true") {
				t.Fatalf("ValidateAlarmWorkerRuntime() error = %v, want %s requirement", err, tt.key)
			}
		})
	}
}

func TestValidateAlarmWorkerRuntimeProductionRequiresYouTubeOutboxDispatcher(t *testing.T) {
	clearRuntimeRoleEnv(t)
	t.Setenv(notificationEgressRoleEnv, notificationEgressRoleOwner)
	t.Setenv(notificationSchedulerRoleEnv, notificationSchedulerRoleWorker)
	t.Setenv(deliveryDispatcherEnabledEnv, "true")
	t.Setenv(alarmDispatchConsumerEnabledEnv, "true")

	err := validRuntimeRoleConfig().ValidateAlarmWorkerRuntime()
	if err == nil || !strings.Contains(err.Error(), "requires YOUTUBE_OUTBOX_DISPATCHER_ENABLED=true") {
		t.Fatalf("ValidateAlarmWorkerRuntime() error = %v, want YouTube outbox dispatcher requirement", err)
	}
}
