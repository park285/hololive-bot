package alarmworker

import (
	"errors"
	"fmt"

	"github.com/kapu/hololive-shared/pkg/config/settings"
	"github.com/kapu/hololive-shared/pkg/config/settings/internal/load"
)

// validateOwnership: alarm-worker만 proactive egress와 scheduler 역할을 소유한다.
// 두 역할은 production에서 명시돼야 하고, 그 밖의 환경에서는 값 검증만 한다.
func validateOwnership(environment string) error {
	if err := load.ValidateNotificationRoleEnvValues(); err != nil {
		return fmt.Errorf("validate notification role env values: %w", err)
	}

	if !load.IsProduction(environment) {
		return nil
	}

	if err := load.RequireNotificationRoleEnv(load.NotificationEgressRoleEnv, load.NotificationEgressRoleOwner); err != nil {
		return fmt.Errorf("require notification egress role env: %w", err)
	}

	if err := load.RequireNotificationRoleEnv(load.NotificationSchedulerRoleEnv, load.NotificationSchedulerRoleWorker, load.NotificationSchedulerRoleOff); err != nil {
		return fmt.Errorf("require notification scheduler role env: %w", err)
	}

	return nil
}

// validateProductionExecutors: production alarm-worker는 모든 worker executor가 켜져 있어야 한다.
func validateProductionExecutors(profile *settings.AlarmWorkerProfile) error {
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
