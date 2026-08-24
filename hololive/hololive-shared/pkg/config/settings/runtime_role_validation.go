package settings

import (
	"errors"
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
	out, err := loadConfigValidated((*Config).ValidateBotRuntime, configLoadOptions{
		WorkerProfileRole: "api",
		TracingRuntime:    tracingRuntimeHololiveAPI,
	})
	if err != nil {
		return nil, fmt.Errorf("load config validated: %w", err)
	}

	return out, nil
}

func LoadAlarmWorkerRuntime() (*Config, error) {
	out, err := loadConfigValidated((*Config).ValidateAlarmWorkerRuntime, configLoadOptions{
		WorkerProfileRole: "alarm-worker",
		TracingRuntime:    tracingRuntimeAlarmWorker,
	})
	if err != nil {
		return nil, fmt.Errorf("load config validated: %w", err)
	}

	return out, nil
}

func (c *Config) ValidateBotRuntime() error {
	if err := c.validateWithRequired(c.validateRequiredConfig); err != nil {
		return fmt.Errorf("validate with required: %w", err)
	}

	if c.APIWorkerProfile == nil {
		return errors.New("bot runtime requires Stack Worker Profile v1")
	}

	if err := validateNoNotificationEgressOwnership(runtimeBot); err != nil {
		return fmt.Errorf("validate no notification egress ownership: %w", err)
	}

	return nil
}

func (c *Config) ValidateAlarmWorkerRuntime() error {
	if err := c.validateWithRequired(c.validateRequiredConfig); err != nil {
		return fmt.Errorf("validate with required: %w", err)
	}

	if c.AlarmWorkerProfile == nil {
		return errors.New("alarm-worker runtime requires Stack Worker Profile v1")
	}

	if err := validateAlarmWorkerOwnership(c.Environment); err != nil {
		return fmt.Errorf("validate alarm worker ownership: %w", err)
	}

	if isProductionEnvironment(c.Environment) {
		if err := validateProductionAlarmExecutors(c.AlarmWorkerProfile); err != nil {
			return fmt.Errorf("validate production alarm executors: %w", err)
		}

		return nil
	}

	return nil
}

func validateProductionAlarmExecutors(profile *AlarmWorkerProfile) error {
	for workerID, worker := range profile.Loaded.Profile.Workers {
		if !worker.Executor.Enabled {
			return fmt.Errorf("alarm-worker production requires %s executor.enabled=true", workerID)
		}
	}

	return nil
}

func validateNoNotificationEgressOwnership(runtime string) error {
	if err := validateNotificationRoleEnvValues(); err != nil {
		//nolint:wrapcheck // 하위 검증 함수가 어떤 환경변수가 잘못됐는지 담은 완결된 메시지를 만들므로, 다시 감싸면 같은 말이 반복된다.
		return err
	}

	if err := rejectReservedEgressRoles(runtime); err != nil {
		//nolint:wrapcheck // 하위 검증 함수가 어떤 환경변수가 잘못됐는지 담은 완결된 메시지를 만들므로, 다시 감싸면 같은 말이 반복된다.
		return err
	}

	return nil
}

func validateNotificationRoleEnvValues() error {
	if err := validateKnownNotificationRoleEnv(notificationEgressRoleEnv, notificationEgressRoleOwner, notificationEgressRoleProducer, notificationEgressRoleOff); err != nil {
		//nolint:wrapcheck // 하위 검증 함수가 어떤 환경변수가 잘못됐는지 담은 완결된 메시지를 만들므로, 다시 감싸면 같은 말이 반복된다.
		return err
	}

	if err := validateKnownNotificationRoleEnv(notificationSchedulerRoleEnv, notificationSchedulerRoleWorker, notificationSchedulerRoleOff); err != nil {
		//nolint:wrapcheck // 하위 검증 함수가 어떤 환경변수가 잘못됐는지 담은 완결된 메시지를 만들므로, 다시 감싸면 같은 말이 반복된다.
		return err
	}

	return nil
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
		//nolint:wrapcheck // 하위 검증 함수가 어떤 환경변수가 잘못됐는지 담은 완결된 메시지를 만들므로, 다시 감싸면 같은 말이 반복된다.
		return err
	}

	if !isProductionEnvironment(environment) {
		return nil
	}

	if err := validateProductionAlarmWorkerOwnership(); err != nil {
		//nolint:wrapcheck // 하위 검증 함수가 어떤 환경변수가 잘못됐는지 담은 완결된 메시지를 만들므로, 다시 감싸면 같은 말이 반복된다.
		return err
	}

	return nil
}

func validateProductionAlarmWorkerOwnership() error {
	if err := requireNotificationRoleEnv(notificationEgressRoleEnv, notificationEgressRoleOwner); err != nil {
		//nolint:wrapcheck // 하위 검증 함수가 어떤 환경변수가 잘못됐는지 담은 완결된 메시지를 만들므로, 다시 감싸면 같은 말이 반복된다.
		return err
	}

	if err := requireNotificationRoleEnv(notificationSchedulerRoleEnv, notificationSchedulerRoleWorker, notificationSchedulerRoleOff); err != nil {
		//nolint:wrapcheck // 하위 검증 함수가 어떤 환경변수가 잘못됐는지 담은 완결된 메시지를 만들므로, 다시 감싸면 같은 말이 반복된다.
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
