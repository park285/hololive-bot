package config

import (
	"errors"
	"fmt"

	sharedenv "github.com/park285/shared-go/core/envutil"

	"github.com/kapu/hololive-shared/pkg/config/runtimepolicy"
)

type NotificationRoles struct {
	Egress    string
	Scheduler string
}

func loadNotificationRoles() NotificationRoles {
	return NotificationRoles{
		Egress:    sharedenv.String(runtimepolicy.NotificationEgressRoleEnv, ""),
		Scheduler: sharedenv.String(runtimepolicy.NotificationSchedulerRoleEnv, ""),
	}
}

func validateOwnership(environment string, roles NotificationRoles) error {
	if err := runtimepolicy.ValidateNotificationRoleEnvValues(roles.Egress, roles.Scheduler); err != nil {
		return fmt.Errorf("validate notification role env values: %w", err)
	}

	if !runtimepolicy.IsProduction(environment) {
		return nil
	}

	if err := runtimepolicy.RequireNotificationRoleEnv(runtimepolicy.RuntimeAlarmWorker, runtimepolicy.NotificationEgressRoleEnv, roles.Egress, runtimepolicy.NotificationEgressRoleOwner); err != nil {
		return fmt.Errorf("require notification egress role env: %w", err)
	}

	if err := runtimepolicy.RequireNotificationRoleEnv(runtimepolicy.RuntimeAlarmWorker, runtimepolicy.NotificationSchedulerRoleEnv, roles.Scheduler, runtimepolicy.NotificationSchedulerRoleWorker, runtimepolicy.NotificationSchedulerRoleOff); err != nil {
		return fmt.Errorf("require notification scheduler role env: %w", err)
	}

	return nil
}

func validateProductionExecutors(profile *AlarmWorkerProfile) error {
	if profile == nil {
		return errors.New("alarm worker profile is nil")
	}

	for workerID, worker := range profile.Loaded.Profile.Workers {
		if !worker.Executor.Enabled {
			return fmt.Errorf("alarm-worker production requires %s executor.enabled=true", workerID)
		}
	}

	return nil
}
