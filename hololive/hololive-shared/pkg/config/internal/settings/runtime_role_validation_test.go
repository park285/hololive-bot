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
		alarmWorkerEgressLeaseEnabledEnv,
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
		Environment: "production",
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

func TestValidateAdminAPIRuntimeRejectsDispatchers(t *testing.T) {
	clearRuntimeRoleEnv(t)
	t.Setenv(deliveryDispatcherEnabledEnv, "true")

	err := validRuntimeRoleConfig().ValidateAdminAPIRuntime()
	if err == nil || !strings.Contains(err.Error(), deliveryDispatcherEnabledEnv) {
		t.Fatalf("ValidateAdminAPIRuntime() error = %v, want delivery dispatcher rejection", err)
	}
}

func TestValidateYouTubeProducerRuntimeRejectsYouTubeOutboxDispatcher(t *testing.T) {
	clearRuntimeRoleEnv(t)
	t.Setenv(youTubeOutboxDispatcherEnabledEnv, "true")

	err := validRuntimeRoleConfig().ValidateYouTubeProducerRuntime()
	if err == nil || !strings.Contains(err.Error(), youTubeOutboxDispatcherEnabledEnv) {
		t.Fatalf("ValidateYouTubeProducerRuntime() error = %v, want YouTube outbox dispatcher rejection", err)
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
	if err == nil || !strings.Contains(err.Error(), "production requires NOTIFICATION_EGRESS_ROLE=owner") {
		t.Fatalf("ValidateAlarmWorkerRuntime() error = %v, want owner role requirement", err)
	}

	clearRuntimeRoleEnv(t)
	t.Setenv(notificationEgressRoleEnv, notificationEgressRoleOwner)
	err = validRuntimeRoleConfig().ValidateAlarmWorkerRuntime()
	if err == nil || !strings.Contains(err.Error(), "production requires NOTIFICATION_SCHEDULER_ROLE=worker") {
		t.Fatalf("ValidateAlarmWorkerRuntime() error = %v, want scheduler worker requirement", err)
	}
}

func TestValidateAlarmWorkerRuntimeProductionAcceptsLeaseProtectedOwner(t *testing.T) {
	clearRuntimeRoleEnv(t)
	t.Setenv(notificationEgressRoleEnv, notificationEgressRoleOwner)
	t.Setenv(notificationSchedulerRoleEnv, notificationSchedulerRoleWorker)
	t.Setenv(alarmWorkerEgressLeaseEnabledEnv, "true")

	if err := validRuntimeRoleConfig().ValidateAlarmWorkerRuntime(); err != nil {
		t.Fatalf("ValidateAlarmWorkerRuntime() error = %v, want nil", err)
	}
}

func TestValidateAlarmWorkerRuntimeProductionRejectsDisabledLease(t *testing.T) {
	clearRuntimeRoleEnv(t)
	t.Setenv(notificationEgressRoleEnv, notificationEgressRoleOwner)
	t.Setenv(notificationSchedulerRoleEnv, notificationSchedulerRoleWorker)
	t.Setenv(alarmWorkerEgressLeaseEnabledEnv, "false")

	err := validRuntimeRoleConfig().ValidateAlarmWorkerRuntime()
	if err == nil || !strings.Contains(err.Error(), "requires ALARM_WORKER_EGRESS_LEASE_ENABLED=true") {
		t.Fatalf("ValidateAlarmWorkerRuntime() error = %v, want egress lease requirement", err)
	}
}
