package settings

import (
	"fmt"
	"os"
	"strings"
)

const (
	runtimeBot              = "bot"
	runtimeAlarmWorker      = "alarm-worker"
	runtimeAdminAPI         = "admin-api"
	runtimeLLMScheduler     = "llm-scheduler"
	runtimeYouTubeCollector = "youtube-collector"

	notificationEgressRoleEnv       = "NOTIFICATION_EGRESS_ROLE"
	notificationSchedulerRoleEnv    = "NOTIFICATION_SCHEDULER_ROLE"
	notificationEgressRoleOwner     = "owner"
	notificationEgressRoleProducer  = "producer"
	notificationEgressRoleOff       = "off"
	notificationSchedulerRoleWorker = "worker"
	notificationSchedulerRoleOff    = "off"

	postgresScraperRoleUser = "hololive_scraper"
	postgresRuntimeRoleUser = "hololive_runtime"
)

// proactive notification egress 소유를 거부하는 bot runtime config 로더다.
func LoadBotRuntime() (*Config, error) {
	return loadConfigValidated((*Config).ValidateBotRuntime, configLoadOptions{
		WorkerProfileRole: "api",
		TracingRuntime:    tracingRuntimeHololiveAPI,
	})
}

func LoadAlarmWorkerRuntime() (*Config, error) {
	return loadConfigValidated((*Config).ValidateAlarmWorkerRuntime, configLoadOptions{
		WorkerProfileRole: "alarm-worker",
		TracingRuntime:    tracingRuntimeAlarmWorker,
	})
}

func (c *Config) ValidateBotRuntime() error {
	if err := c.validateWithRequired(c.validateRequiredConfig); err != nil {
		return err
	}
	if c.APIWorkerProfile == nil {
		return fmt.Errorf("bot runtime requires Stack Worker Profile v1")
	}
	return validateNoNotificationEgressOwnership(runtimeBot)
}

func (c *Config) ValidateAlarmWorkerRuntime() error {
	if err := c.validateWithRequired(c.validateRequiredConfig); err != nil {
		return err
	}
	if c.AlarmWorkerProfile == nil {
		return fmt.Errorf("alarm-worker runtime requires Stack Worker Profile v1")
	}
	if err := validateAlarmWorkerOwnership(c.Environment); err != nil {
		return err
	}
	if isProductionEnvironment(c.Environment) {
		for workerID, worker := range c.AlarmWorkerProfile.Loaded.Profile.Workers {
			if !worker.Executor.Enabled {
				return fmt.Errorf("alarm-worker production requires %s executor.enabled=true", workerID)
			}
		}
	}
	return nil
}

func validateNoNotificationEgressOwnership(runtime string) error {
	if err := validateNotificationRoleEnvValues(); err != nil {
		return err
	}
	if err := rejectReservedEgressRoles(runtime); err != nil {
		return err
	}
	return nil
}

func validateNotificationRoleEnvValues() error {
	if err := validateKnownNotificationRoleEnv(notificationEgressRoleEnv, notificationEgressRoleOwner, notificationEgressRoleProducer, notificationEgressRoleOff); err != nil {
		return err
	}
	return validateKnownNotificationRoleEnv(notificationSchedulerRoleEnv, notificationSchedulerRoleWorker, notificationSchedulerRoleOff)
}

func rejectReservedEgressRoles(runtime string) error {
	if matchesNotificationRole(trimmedEnv(notificationEgressRoleEnv), notificationEgressRoleOwner) {
		return fmt.Errorf("%s must not own proactive notification egress; %s=%s is reserved for alarm-worker", runtime, notificationEgressRoleEnv, notificationEgressRoleOwner)
	}
	if matchesNotificationRole(trimmedEnv(notificationSchedulerRoleEnv), notificationSchedulerRoleWorker) {
		return fmt.Errorf("%s must not run the alarm scheduler role; %s=%s is reserved for alarm-worker", runtime, notificationSchedulerRoleEnv, notificationSchedulerRoleWorker)
	}
	return nil
}

func validateAlarmWorkerOwnership(environment string) error {
	if err := validateNotificationRoleEnvValues(); err != nil {
		return err
	}
	if !isProductionEnvironment(environment) {
		return nil
	}
	return validateProductionAlarmWorkerOwnership()
}

func validateProductionAlarmWorkerOwnership() error {
	if err := requireNotificationRoleEnv(notificationEgressRoleEnv, notificationEgressRoleOwner); err != nil {
		return err
	}
	if err := requireNotificationRoleEnv(notificationSchedulerRoleEnv, notificationSchedulerRoleWorker, notificationSchedulerRoleOff); err != nil {
		return err
	}
	return nil
}

func validateKnownNotificationRoleEnv(key string, allowed ...string) error {
	value := trimmedEnv(key)
	if value == "" {
		return nil
	}
	if matchesNotificationRole(value, allowed...) {
		return nil
	}
	return fmt.Errorf("unsupported %s=%s", key, value)
}

func requireNotificationRoleEnv(key string, allowed ...string) error {
	if matchesNotificationRole(trimmedEnv(key), allowed...) {
		return nil
	}
	return fmt.Errorf("%s production requires %s=%s", runtimeAlarmWorker, key, strings.Join(allowed, "|"))
}

func matchesNotificationRole(value string, allowed ...string) bool {
	for _, candidate := range allowed {
		if strings.EqualFold(value, candidate) {
			return true
		}
	}
	return false
}

func trimmedEnv(key string) string {
	return strings.TrimSpace(os.Getenv(key))
}

func validateYouTubeCollectorPostgresUser(user string) error {
	want := resolvedHololiveScraperUser()
	if strings.TrimSpace(user) != want {
		return fmt.Errorf("%s requires POSTGRES_USER=%s", runtimeYouTubeCollector, want)
	}
	return nil
}
