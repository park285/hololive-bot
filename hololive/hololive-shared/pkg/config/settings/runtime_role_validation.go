package settings

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

const (
	runtimeBot             = "bot"
	runtimeAlarmWorker     = "alarm-worker"
	runtimeAdminAPI        = "admin-api"
	runtimeLLMScheduler    = "llm-scheduler"
	runtimeYouTubeProducer = "youtube-producer"

	notificationEgressRoleEnv         = "NOTIFICATION_EGRESS_ROLE"
	notificationSchedulerRoleEnv      = "NOTIFICATION_SCHEDULER_ROLE"
	deliveryDispatcherEnabledEnv      = "DELIVERY_DISPATCHER_ENABLED"
	youTubeOutboxDispatcherEnabledEnv = "YOUTUBE_OUTBOX_DISPATCHER_ENABLED"
	alarmDispatchConsumerEnabledEnv   = "ALARM_DISPATCH_CONSUMER_ENABLED"
	alarmWorkerEgressLeaseEnabledEnv  = "ALARM_WORKER_EGRESS_LEASE_ENABLED"

	notificationEgressRoleOwner     = "owner"
	notificationEgressRoleProducer  = "producer"
	notificationEgressRoleOff       = "off"
	notificationSchedulerRoleWorker = "worker"
	notificationSchedulerRoleOff    = "off"
)

// proactive notification egress 소유를 거부하는 bot runtime config 로더다. 구버전
// 호출부를 위해 legacy Load 함수를 의도적으로 남겨두지만, binary는 runtime별 로더를
// 써야 ownership drift가 startup에서 실패한다.
func LoadBotRuntime() (*Config, error) {
	return loadConfigValidated((*Config).ValidateBotRuntime, configLoadOptions{FetchIrisWorkerProfile: true})
}

// alarm-worker config를 로드하고, production runtime이 유일한 notification egress
// owner인지 검증한다.
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
	if strings.EqualFold(trimmedEnv(notificationEgressRoleEnv), notificationEgressRoleOwner) {
		return fmt.Errorf("%s must not own proactive notification egress; %s=%s is reserved for alarm-worker", runtime, notificationEgressRoleEnv, notificationEgressRoleOwner)
	}
	if strings.EqualFold(trimmedEnv(notificationSchedulerRoleEnv), notificationSchedulerRoleWorker) {
		return fmt.Errorf("%s must not run the alarm scheduler role; %s=%s is reserved for alarm-worker", runtime, notificationSchedulerRoleEnv, notificationSchedulerRoleWorker)
	}
	return nil
}

func rejectReservedDispatchers(runtime string) error {
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
	if err := requireNotificationRoleEnv(notificationSchedulerRoleEnv, notificationSchedulerRoleWorker); err != nil {
		return err
	}
	if err := requireBoolEnvNotFalse(alarmWorkerEgressLeaseEnabledEnv, "proactive egress has a single owner lease"); err != nil {
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
	for _, candidate := range allowed {
		if strings.EqualFold(value, candidate) {
			return nil
		}
	}
	return fmt.Errorf("unsupported %s=%s", key, value)
}

func requireNotificationRoleEnv(key, expected string) error {
	if strings.EqualFold(trimmedEnv(key), expected) {
		return nil
	}
	return fmt.Errorf("%s production requires %s=%s", runtimeAlarmWorker, key, expected)
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
