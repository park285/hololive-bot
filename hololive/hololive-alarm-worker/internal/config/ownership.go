package config

import (
	"errors"
	"fmt"

	"github.com/kapu/hololive-shared/pkg/config/envload"
	"github.com/kapu/hololive-shared/pkg/config/runtimepolicy"
)

// validateOwnership: alarm-worker만 proactive egress와 scheduler 역할을 소유한다.
// 두 역할은 production에서 명시돼야 하고, 그 밖의 환경에서는 값 검증만 한다.
func validateOwnership(environment string) error {
	egressRole := envload.TrimmedEnv(runtimepolicy.NotificationEgressRoleEnv)
	schedulerRole := envload.TrimmedEnv(runtimepolicy.NotificationSchedulerRoleEnv)

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

// validateProductionExecutors는 각 발송 파이프라인의 executor가 모두 활성화되었는지 확인한다.
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
