package config

import (
	"errors"
	"fmt"

	sharedenv "github.com/park285/shared-go/v2/pkg/envutil"

	"github.com/kapu/hololive-shared/pkg/config/runtimepolicy"
)

// alarm-worker만 proactive egress와 scheduler 역할을 소유한다.
func validateOwnership(environment string) error {
	egressRole := sharedenv.String(runtimepolicy.NotificationEgressRoleEnv, "")
	schedulerRole := sharedenv.String(runtimepolicy.NotificationSchedulerRoleEnv, "")

	if err := runtimepolicy.ValidateNotificationRoleEnvValues(egressRole, schedulerRole); err != nil {
		return fmt.Errorf("validate notification role env values: %w", err)
	}

	if !runtimepolicy.IsProduction(environment) {
		return nil
	}

	if err := runtimepolicy.RequireNotificationRoleEnv(runtimepolicy.NotificationEgressRoleEnv, egressRole, runtimepolicy.NotificationEgressRoleOwner); err != nil {
		return fmt.Errorf("require notification egress role env: %w", err)
	}

	if err := runtimepolicy.RequireNotificationRoleEnv(runtimepolicy.NotificationSchedulerRoleEnv, schedulerRole, runtimepolicy.NotificationSchedulerRoleWorker, runtimepolicy.NotificationSchedulerRoleOff); err != nil {
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
