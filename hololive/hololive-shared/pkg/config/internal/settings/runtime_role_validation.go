package settings

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

const (
	runtimeBot         = "bot"
	runtimeAlarmWorker = "alarm-worker"
	runtimeAdminAPI    = "admin-api"
	runtimeLLMScheduler = "llm-scheduler"
	runtimeYouTubeProducer = "youtube-producer"

	notificationEgressRoleEnv       = "NOTIFICATION_EGRESS_ROLE"
	notificationSchedulerRoleEnv    = "NOTIFICATION_SCHEDULER_ROLE"
	deliveryDispatcherEnabledEnv     = "DELIVERY_DISPATCHER_ENABLED"
	youTubeOutboxDispatcherEnabledEnv = "YOUTUBE_OUTBOX_DISPATCHER_ENABLED"
	alarmDispatchConsumerEnabledEnv = "ALARM_DISPATCH_CONSUMER_ENABLED"
	alarmWorkerEgressLeaseEnabledEnv = "ALARM_WORKER_EGRESS_LEASE_ENABLED"

	notificationEgressRoleOwner    = "owner"
	notificationEgressRoleProducer = "producer"
	notificationEgressRoleOff      = "off"
	notificationSchedulerRoleWorker = "worker"
	notificationSchedulerRoleOff    = "off"
)

// LoadBotRuntime loads the user-facing Kakao/Iris bot runtime config and rejects
// proactive notification egress ownership. It intentionally keeps the legacy
// Load function available for older callers, but binaries should use the
// runtime-specific loader so ownership drift fails at startup.
func LoadBotRuntime() (*Config, error) {
	return loadConfigValidated((*Config).ValidateBotRuntime, configLoadOptions{FetchIrisWorkerProfile: true})
}

// LoadAlarmWorkerRuntime loads the alarm-worker config and validates that the
// production runtime is the only notification egress owner.
func LoadAlarmWorkerRuntime() (*Config, error) {
	return loadConfigValidated((*Config).ValidateAlarmWorkerRuntime, configLoadOptions{FetchIrisWorkerProfile: true})
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
	if err := validateKnownNotificationRoleEnv(notificationEgressRoleEnv, notificationEgressRoleOwner, notificationEgressRoleProducer, notificationEgressRoleOff); err != nil {
		return err
	}
	if err := validateKnownNotificationRoleEnv(notificationSchedulerRoleEnv, notificationSchedulerRoleWorker, notificationSchedulerRoleOff); err != nil {
		return err
	}

	if strings.EqualFold(trimmedEnv(notificationEgressRoleEnv), notificationEgressRoleOwner) {
		return fmt.Errorf("%s must not own proactive notification egress; %s=%s is reserved for alarm-worker", runtime, notificationEgressRoleEnv, notificationEgressRoleOwner)
	}
	if strings.EqualFold(trimmedEnv(notificationSchedulerRoleEnv), notificationSchedulerRoleWorker) {
		return fmt.Errorf("%s must not run the alarm scheduler role; %s=%s is reserved for alarm-worker", runtime, notificationSchedulerRoleEnv, notificationSchedulerRoleWorker)
	}

	for _, key := range []string{
		deliveryDispatcherEnabledEnv,
		youTubeOutboxDispatcherEnabledEnv,
		alarmDispatchConsumerEnabledEnv,
		alarmWorkerEgressLeaseEnabledEnv,
	} {
		if err := rejectExplicitTrueEnv(runtime, key); err != nil {
			return err
		}
	}

	return nil
}

func validateAlarmWorkerOwnership(environment string) error {
	if err := validateKnownNotificationRoleEnv(notificationEgressRoleEnv, notificationEgressRoleOwner, notificationEgressRoleProducer, notificationEgressRoleOff); err != nil {
		return err
	}
	if err := validateKnownNotificationRoleEnv(notificationSchedulerRoleEnv, notificationSchedulerRoleWorker, notificationSchedulerRoleOff); err != nil {
		return err
	}
	if !isProductionEnvironment(environment) {
		return nil
	}

	if !strings.EqualFold(trimmedEnv(notificationEgressRoleEnv), notificationEgressRoleOwner) {
		return fmt.Errorf("%s production requires %s=%s", runtimeAlarmWorker, notificationEgressRoleEnv, notificationEgressRoleOwner)
	}
	if !strings.EqualFold(trimmedEnv(notificationSchedulerRoleEnv), notificationSchedulerRoleWorker) {
		return fmt.Errorf("%s production requires %s=%s", runtimeAlarmWorker, notificationSchedulerRoleEnv, notificationSchedulerRoleWorker)
	}
	if disabled, err := explicitBoolEnvIsFalse(alarmWorkerEgressLeaseEnabledEnv); err != nil {
		return err
	} else if disabled {
		return fmt.Errorf("%s production requires %s=true so proactive egress has a single owner lease", runtimeAlarmWorker, alarmWorkerEgressLeaseEnabledEnv)
	}

	return nil
}

func validateKnownNotificationRoleEnv(key string, allowed ...string) error {
	value := trimmedEnv(key)
	if value == "" {
		return nil
	}
	for _, candidate := range allowed {
		if strings.EqualFold(value, candidate) {
			return nil
		}
	}
	return fmt.Errorf("unsupported %s=%s", key, value)
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

func lookupExplicitBoolEnv(key string) (bool, bool, error) {
	raw, ok := os.LookupEnv(key)
	if !ok {
		return false, false, nil
	}
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return false, false, nil
	}
	value, err := strconv.ParseBool(trimmed)
	if err != nil {
		return false, true, fmt.Errorf("%s must be a boolean: %w", key, err)
	}
	return value, true, nil
}

func trimmedEnv(key string) string {
	return strings.TrimSpace(os.Getenv(key))
}
