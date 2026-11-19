package runtimepolicy

import (
	"fmt"
	"strings"
)

const (
	RuntimeBot              = "bot"
	RuntimeAlarmWorker      = "alarm-worker"
	RuntimeAdminAPI         = "admin-api"
	RuntimeYouTubeCollector = "youtube-collector"

	NotificationEgressRoleEnv       = "NOTIFICATION_EGRESS_ROLE"
	NotificationSchedulerRoleEnv    = "NOTIFICATION_SCHEDULER_ROLE"
	NotificationEgressRoleOwner     = "owner"
	NotificationEgressRoleProducer  = "producer"
	NotificationEgressRoleOff       = "off"
	NotificationSchedulerRoleWorker = "worker"
	NotificationSchedulerRoleOff    = "off"

	PostgresScraperRoleUser     = "hololive_scraper"
	PostgresRuntimeRoleUser     = "hololive_runtime"
	PostgresAlarmWorkerRoleUser = "hololive_alarm_worker"
	PostgresCollectorARoleUser  = "hololive_collector_a"
	PostgresCollectorBRoleUser  = "hololive_collector_b"
	PostgresCollectorCRoleUser  = "hololive_collector_c"
	PostgresCollectorDRoleUser  = "hololive_collector_d"
)

func PostgresCollectorRoleUser(instanceID string) (string, bool) {
	switch instanceID {
	case "youtube-collector-a":
		return PostgresCollectorARoleUser, true
	case "youtube-collector-b":
		return PostgresCollectorBRoleUser, true
	case "youtube-collector-c":
		return PostgresCollectorCRoleUser, true
	case "youtube-collector-d":
		return PostgresCollectorDRoleUser, true
	default:
		return "", false
	}
}

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

func RequireNotificationRoleEnv(runtime, key, value string, allowed ...string) error {
	if MatchesNotificationRole(value, allowed...) {
		return nil
	}

	return fmt.Errorf("%s production requires %s=%s", runtime, key, strings.Join(allowed, "|"))
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
