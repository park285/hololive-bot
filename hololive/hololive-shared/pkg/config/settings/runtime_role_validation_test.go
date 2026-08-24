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
		"MEMBER_NEWS_CLIPROXY_MODEL",
		"DB_SSLMODE",
		"DB_QUERY_EXEC_MODE",
		"OTEL_ENVIRONMENT",
	} {
		unsetEnvForTest(t, key)
	}
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
		Postgres: PostgresConfig{SSLMode: postgresSSLModeVerifyFull},
		Scraper: ScraperConfig{
			FetcherEngine: ScraperFetcherEngineNetHTTP,
			Backfill:      ScraperBackfillConfig{TargetGroup: "notification"},
		},
		OfficialSchedule:     DefaultOfficialScheduleConfig(),
		MaxResponseBodyBytes: DefaultMaxResponseBodyBytes,
		Environment:          environmentProduction,
		YouTubeCollector:     YouTubeCollectorConfig{InstanceID: collectorInstanceC},
		APIWorkerProfile:     apiWorkerProfileFixture(t),
		AlarmWorkerProfile:   alarmWorkerProfileFixture(t),
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
			H3KeyFile:      hololiveH3KeyPath,
		},
		Postgres:    PostgresConfig{SSLMode: postgresSSLModeVerifyFull},
		Environment: environmentProduction,
	}
}

func TestValidateBotRuntimeRejectsNotificationEgressOwner(t *testing.T) {
	clearRuntimeRoleEnv(t)
	t.Setenv(notificationEgressRoleEnv, notificationEgressRoleOwner)

	err := validRuntimeRoleConfig(t).ValidateBotRuntime()
	if err == nil || !strings.Contains(err.Error(), "must not own proactive notification egress") {
		t.Fatalf("ValidateBotRuntime() error = %v, want proactive egress ownership rejection", err)
	}
}

func TestValidateBotRuntimeRejectsMixedCaseNotificationEgressOwner(t *testing.T) {
	clearRuntimeRoleEnv(t)
	t.Setenv(notificationEgressRoleEnv, "Owner")

	err := validRuntimeRoleConfig(t).ValidateBotRuntime()
	if err == nil || err.Error() != "validate no notification egress ownership: bot must not own proactive notification egress; NOTIFICATION_EGRESS_ROLE=owner is reserved for alarm-worker" {
		t.Fatalf("ValidateBotRuntime() error = %v, want proactive egress ownership rejection", err)
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

	err := validRuntimeRoleConfig(t).ValidateAlarmWorkerRuntime()
	if err == nil || err.Error() != "validate alarm worker ownership: alarm-worker production requires NOTIFICATION_EGRESS_ROLE=owner" {
		t.Fatalf("ValidateAlarmWorkerRuntime() error = %v, want owner role requirement", err)
	}

	clearRuntimeRoleEnv(t)
	t.Setenv(notificationEgressRoleEnv, notificationEgressRoleOwner)

	err = validRuntimeRoleConfig(t).ValidateAlarmWorkerRuntime()
	if err == nil || err.Error() != "validate alarm worker ownership: alarm-worker production requires NOTIFICATION_SCHEDULER_ROLE=worker|off" {
		t.Fatalf("ValidateAlarmWorkerRuntime() error = %v, want scheduler role enumeration requirement", err)
	}
}

func TestValidateAlarmWorkerRuntimeProductionRejectsNonOwnerEgressRoles(t *testing.T) {
	for _, role := range []string{notificationEgressRoleProducer, notificationEgressRoleOff} {
		t.Run(role, func(t *testing.T) {
			clearRuntimeRoleEnv(t)
			t.Setenv(notificationEgressRoleEnv, role)
			t.Setenv(notificationSchedulerRoleEnv, notificationSchedulerRoleWorker)

			err := validRuntimeRoleConfig(t).ValidateAlarmWorkerRuntime()
			if err == nil || err.Error() != "validate alarm worker ownership: alarm-worker production requires NOTIFICATION_EGRESS_ROLE=owner" {
				t.Fatalf("ValidateAlarmWorkerRuntime() error = %v, want owner role requirement", err)
			}
		})
	}
}

func TestValidateAlarmWorkerRuntimeNonProductionSkipsOwnershipRequirements(t *testing.T) {
	clearRuntimeRoleEnv(t)

	cfg := validRuntimeRoleConfig(t)

	cfg.Environment = "staging"

	if err := cfg.ValidateAlarmWorkerRuntime(); err != nil {
		t.Fatalf("ValidateAlarmWorkerRuntime() error = %v, want nil", err)
	}
}

func TestValidateAlarmWorkerRuntimeProductionAcceptsSchedulerWorkerProfile(t *testing.T) {
	clearRuntimeRoleEnv(t)
	t.Setenv(notificationEgressRoleEnv, notificationEgressRoleOwner)
	t.Setenv(notificationSchedulerRoleEnv, notificationSchedulerRoleWorker)

	if err := validRuntimeRoleConfig(t).ValidateAlarmWorkerRuntime(); err != nil {
		t.Fatalf("ValidateAlarmWorkerRuntime() error = %v, want nil", err)
	}
}

func TestValidateAlarmWorkerRuntimeProductionAcceptsEgressOnlyProfile(t *testing.T) {
	clearRuntimeRoleEnv(t)
	t.Setenv(notificationEgressRoleEnv, notificationEgressRoleOwner)
	t.Setenv(notificationSchedulerRoleEnv, notificationSchedulerRoleOff)

	if err := validRuntimeRoleConfig(t).ValidateAlarmWorkerRuntime(); err != nil {
		t.Fatalf("ValidateAlarmWorkerRuntime() error = %v, want nil", err)
	}
}

func TestValidateAlarmWorkerRuntimeRejectsUnsupportedSchedulerRole(t *testing.T) {
	clearRuntimeRoleEnv(t)
	t.Setenv(notificationEgressRoleEnv, notificationEgressRoleOwner)
	t.Setenv(notificationSchedulerRoleEnv, "bot")

	err := validRuntimeRoleConfig(t).ValidateAlarmWorkerRuntime()
	if err == nil || err.Error() != "validate alarm worker ownership: unsupported NOTIFICATION_SCHEDULER_ROLE=bot" {
		t.Fatalf("ValidateAlarmWorkerRuntime() error = %v, want unsupported scheduler role rejection", err)
	}
}

func TestValidateAlarmWorkerRuntimeProductionRejectsDisabledProfileExecutor(t *testing.T) {
	for _, workerID := range []string{"alarm_dispatch", "notification_delivery", "youtube_delivery"} {
		t.Run(workerID, func(t *testing.T) {
			clearRuntimeRoleEnv(t)
			t.Setenv(notificationEgressRoleEnv, notificationEgressRoleOwner)
			t.Setenv(notificationSchedulerRoleEnv, notificationSchedulerRoleWorker)

			cfg := validRuntimeRoleConfig(t)
			worker := cfg.AlarmWorkerProfile.Loaded.Profile.Workers[workerID]

			worker.Executor.Enabled = false
			cfg.AlarmWorkerProfile.Loaded.Profile.Workers[workerID] = worker

			err := cfg.ValidateAlarmWorkerRuntime()
			if err == nil || !strings.Contains(err.Error(), "requires "+workerID+" executor.enabled=true") {
				t.Fatalf("ValidateAlarmWorkerRuntime() error = %v, want %s profile requirement", err, workerID)
			}
		})
	}
}
