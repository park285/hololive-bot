package settings

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

const (
	runtimeBot              = "bot"
	runtimeAlarmWorker      = "alarm-worker"
	runtimeAdminAPI         = "admin-api"
	runtimeLLMScheduler     = "llm-scheduler"
	runtimeYouTubeProducer  = "youtube-producer"
	runtimeYouTubeCollector = "youtube-collector"

	notificationEgressRoleEnv         = "NOTIFICATION_EGRESS_ROLE"
	notificationSchedulerRoleEnv      = "NOTIFICATION_SCHEDULER_ROLE"
	deliveryDispatcherEnabledEnv      = "DELIVERY_DISPATCHER_ENABLED"
	youTubeOutboxDispatcherEnabledEnv = "YOUTUBE_OUTBOX_DISPATCHER_ENABLED"
	alarmDispatchConsumerEnabledEnv   = "ALARM_DISPATCH_CONSUMER_ENABLED"

	notificationEgressRoleOwner     = "owner"
	notificationEgressRoleProducer  = "producer"
	notificationEgressRoleOff       = "off"
	notificationSchedulerRoleWorker = "worker"
	notificationSchedulerRoleOff    = "off"

	postgresScraperRoleUser = "hololive_scraper"
)

// proactive notification egress 소유를 거부하는 bot runtime config 로더다.
func LoadBotRuntime() (*Config, error) {
	return loadConfigValidated((*Config).ValidateBotRuntime, configLoadOptions{
		FetchIrisWorkerProfile: true,
		TracingRuntime:         tracingRuntimeHololiveAPI,
	})
}

func LoadAlarmWorkerRuntime() (*Config, error) {
	return loadConfigValidated((*Config).ValidateAlarmWorkerRuntime, configLoadOptions{
		FetchIrisWorkerProfile: true,
		TracingRuntime:         tracingRuntimeAlarmWorker,
	})
}

func (c *Config) ValidateBotRuntime() error {
	if err := c.validateWithRequired(c.validateRequiredConfig); err != nil {
		return err
	}
	return validateNoNotificationEgressOwnership(runtimeBot)
}

func (c *Config) ValidateAlarmWorkerRuntime() error {
	if err := c.validateWithRequired(c.validateRequiredConfig); err != nil {
		return err
	}
	return validateAlarmWorkerOwnership(c.Environment)
}

func validateNoNotificationEgressOwnership(runtime string) error {
	if err := validateNotificationRoleEnvValues(); err != nil {
		return err
	}
	if err := rejectReservedEgressRoles(runtime); err != nil {
		return err
	}
	return rejectReservedDispatchers(runtime)
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

func rejectReservedDispatchers(runtime string) error {
	for _, key := range []string{
		deliveryDispatcherEnabledEnv,
		youTubeOutboxDispatcherEnabledEnv,
		alarmDispatchConsumerEnabledEnv,
	} {
		if err := rejectExplicitTrueEnv(runtime, key); err != nil {
			return err
		}
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
	if err := requireBoolEnvNotFalse(deliveryDispatcherEnabledEnv, "generic notification delivery outbox egress runs"); err != nil {
		return err
	}
	if err := requireBoolEnvNotFalse(alarmDispatchConsumerEnabledEnv, "alarm dispatch outbox egress runs"); err != nil {
		return err
	}
	if err := requireExplicitTrueBoolEnv(youTubeOutboxDispatcherEnabledEnv, "YouTube outbox egress runs"); err != nil {
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

func rejectExplicitTrueEnv(runtime, key string) error {
	value, explicit, err := lookupExplicitBoolEnv(key)
	if err != nil {
		return err
	}
	if explicit && value {
		return fmt.Errorf("%s must not enable %s=true; proactive notification egress is owned by alarm-worker", runtime, key)
	}
	return nil
}

func explicitBoolEnvIsFalse(key string) (bool, error) {
	value, explicit, err := lookupExplicitBoolEnv(key)
	if err != nil {
		return false, err
	}
	return explicit && !value, nil
}

func requireBoolEnvNotFalse(key, purpose string) error {
	disabled, err := explicitBoolEnvIsFalse(key)
	if err != nil {
		return err
	}
	if disabled {
		return fmt.Errorf("%s production requires %s=true so %s", runtimeAlarmWorker, key, purpose)
	}
	return nil
}

func requireExplicitTrueBoolEnv(key, purpose string) error {
	value, explicit, err := lookupExplicitBoolEnv(key)
	if err != nil {
		return err
	}
	if !explicit || !value {
		return fmt.Errorf("%s production requires %s=true so %s", runtimeAlarmWorker, key, purpose)
	}
	return nil
}

func lookupExplicitBoolEnv(key string) (value, explicit bool, err error) {
	raw, ok := os.LookupEnv(key)
	if !ok {
		return false, false, nil
	}
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return false, false, nil
	}
	value, err = strconv.ParseBool(trimmed)
	if err != nil {
		return false, true, fmt.Errorf("%s must be a boolean: %w", key, err)
	}
	return value, true, nil
}

func trimmedEnv(key string) string {
	return strings.TrimSpace(os.Getenv(key))
}

func validateYouTubeCollectorPostgresUser(user string) error {
	if strings.TrimSpace(user) != postgresScraperRoleUser {
		return fmt.Errorf("%s requires POSTGRES_USER=%s", runtimeYouTubeCollector, postgresScraperRoleUser)
	}
	return nil
}

func validateYouTubeProducerPostgresUser(user string) error {
	if strings.TrimSpace(user) == postgresScraperRoleUser {
		return fmt.Errorf("%s must not use POSTGRES_USER=%s", runtimeYouTubeProducer, postgresScraperRoleUser)
	}
	return nil
}
