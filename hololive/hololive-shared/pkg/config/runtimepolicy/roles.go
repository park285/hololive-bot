package runtimepolicy

import (
	"fmt"
	"strings"
)

const (
	RuntimeBot              = "bot"
	RuntimeAlarmWorker      = "alarm-worker"
	RuntimeAdminAPI         = "admin-api"
	RuntimeLLMScheduler     = "llm-scheduler"
	RuntimeYouTubeCollector = "youtube-collector"

	NotificationEgressRoleEnv       = "NOTIFICATION_EGRESS_ROLE"
	NotificationSchedulerRoleEnv    = "NOTIFICATION_SCHEDULER_ROLE"
	NotificationEgressRoleOwner     = "owner"
	NotificationEgressRoleProducer  = "producer"
	NotificationEgressRoleOff       = "off"
	NotificationSchedulerRoleWorker = "worker"
	NotificationSchedulerRoleOff    = "off"

	PostgresScraperRoleUser = "hololive_scraper"
	PostgresRuntimeRoleUser = "hololive_runtime"
)

// ValidateNoNotificationEgressOwnership: alarm-worker가 아닌 runtime이 proactive egress를 소유하지 못하게 한다.
func ValidateNoNotificationEgressOwnership(runtime, egressRole, schedulerRole string) error {
	if err := ValidateNotificationRoleEnvValues(egressRole, schedulerRole); err != nil {
		return err
	}

	if err := RejectReservedEgressRoles(runtime, egressRole, schedulerRole); err != nil {
		return err
	}

	return nil
}

func ValidateNotificationRoleEnvValues(egressRole, schedulerRole string) error {
	if err := validateKnownNotificationRoleEnv(NotificationEgressRoleEnv, egressRole, NotificationEgressRoleOwner, NotificationEgressRoleProducer, NotificationEgressRoleOff); err != nil {
		return err
	}

	if err := validateKnownNotificationRoleEnv(NotificationSchedulerRoleEnv, schedulerRole, NotificationSchedulerRoleWorker, NotificationSchedulerRoleOff); err != nil {
		return err
	}

	return nil
}

func RejectReservedEgressRoles(runtime, egressRole, schedulerRole string) error {
	if MatchesNotificationRole(egressRole, NotificationEgressRoleOwner) {
		return fmt.Errorf("%s must not own proactive notification egress; %s=%s is reserved for alarm-worker", runtime, NotificationEgressRoleEnv, NotificationEgressRoleOwner)
	}

	if MatchesNotificationRole(schedulerRole, NotificationSchedulerRoleWorker) {
		return fmt.Errorf("%s must not run the alarm scheduler role; %s=%s is reserved for alarm-worker", runtime, NotificationSchedulerRoleEnv, NotificationSchedulerRoleWorker)
	}

	return nil
}

// RequireNotificationRoleEnv: alarm-worker production이 소유 역할을 명시하도록 강제한다.
func RequireNotificationRoleEnv(key, value string, allowed ...string) error {
	if MatchesNotificationRole(value, allowed...) {
		return nil
	}

	return fmt.Errorf("%s production requires %s=%s", RuntimeAlarmWorker, key, strings.Join(allowed, "|"))
}

func validateKnownNotificationRoleEnv(key, value string, allowed ...string) error {
	if value == "" {
		return nil
	}

	if MatchesNotificationRole(value, allowed...) {
		return nil
	}

	return fmt.Errorf("unsupported %s=%s", key, value)
}

func MatchesNotificationRole(value string, allowed ...string) bool {
	for _, candidate := range allowed {
		if strings.EqualFold(value, candidate) {
			return true
		}
	}

	return false
}
