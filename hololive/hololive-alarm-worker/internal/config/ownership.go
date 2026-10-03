package config

import (
	"errors"
	"fmt"

	"github.com/kapu/hololive-shared/pkg/config/envload"
	"github.com/kapu/hololive-shared/pkg/config/runtimepolicy"
	"github.com/kapu/hololive-shared/pkg/config/settings"
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

// validateProductionExecutors: production alarm-worker는 모든 worker executor가 켜져 있어야 한다.
// DEC-20260926-hololive-outbox-v3-convergence로 v1(youtube_delivery)과 v2(notification_delivery)는 v3(alarm_dispatch)로
// 넘기지 않는 정본 파이프라인이므로 세 executor가 모두 각자의 발송을 소유한다. 그래서 v3 cutover 뒤 v2 executor를 끄는 절차는
// handoff와 함께 없어졌다.
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
