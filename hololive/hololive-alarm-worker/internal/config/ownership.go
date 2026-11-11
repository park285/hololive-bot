package config

import (
	"errors"
	"fmt"

	sharedenv "github.com/park285/shared-go/v2/pkg/envutil"

	"github.com/kapu/hololive-shared/pkg/config/runtimepolicy"
)

// NotificationRoles는 NOTIFICATION_EGRESS_ROLE·NOTIFICATION_SCHEDULER_ROLE의 앞뒤 공백을 지운 값이다.
// 빈 값은 설정되지 않았음을 뜻하고, 허용값과 production 필수 여부는 validateOwnership이 검사한다.
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

// validateOwnership: alarm-worker만 proactive egress와 scheduler 역할을 소유한다.
// 두 역할은 production에서 명시돼야 하고, 그 밖의 환경에서는 값 검증만 한다.
func validateOwnership(environment string, roles NotificationRoles) error {
	if err := runtimepolicy.ValidateNotificationRoleEnvValues(roles.Egress, roles.Scheduler); err != nil {
		return fmt.Errorf("validate notification role env values: %w", err)
	}

	if !runtimepolicy.IsProduction(environment) {
		return nil
	}

	if err := runtimepolicy.RequireNotificationRoleEnv(runtimepolicy.NotificationEgressRoleEnv, roles.Egress, runtimepolicy.NotificationEgressRoleOwner); err != nil {
		return fmt.Errorf("require notification egress role env: %w", err)
	}

	if err := runtimepolicy.RequireNotificationRoleEnv(runtimepolicy.NotificationSchedulerRoleEnv, roles.Scheduler, runtimepolicy.NotificationSchedulerRoleWorker, runtimepolicy.NotificationSchedulerRoleOff); err != nil {
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
