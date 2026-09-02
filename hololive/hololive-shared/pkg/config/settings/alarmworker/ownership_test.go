package alarmworker

import (
	"strings"
	"testing"

	"github.com/kapu/hololive-shared/pkg/config/settings"
	"github.com/kapu/hololive-shared/pkg/config/settings/internal/load"
	"github.com/kapu/hololive-shared/pkg/config/settings/internal/settingstest"
)

func alarmWorkerProfileFixture(t *testing.T) *settings.AlarmWorkerProfile {
	t.Helper()
	settingstest.UseProfileFixture(t, "stack-worker-profile-alarm-worker.json")

	profile, err := LoadWorkerProfile()
	if err != nil {
		t.Fatalf("load alarm worker profile fixture: %v", err)
	}

	return profile
}

func TestValidateAlarmWorkerOwnershipProductionRequiresOwnerWorkerRoles(t *testing.T) {
	settingstest.ClearRuntimeRoleEnv(t)

	err := validateOwnership(load.EnvironmentProduction)
	if err == nil || !strings.Contains(err.Error(), "alarm-worker production requires NOTIFICATION_EGRESS_ROLE=owner") {
		t.Fatalf("validateOwnership() error = %v, want owner role requirement", err)
	}

	settingstest.ClearRuntimeRoleEnv(t)
	t.Setenv(load.NotificationEgressRoleEnv, load.NotificationEgressRoleOwner)

	err = validateOwnership(load.EnvironmentProduction)
	if err == nil || !strings.Contains(err.Error(), "alarm-worker production requires NOTIFICATION_SCHEDULER_ROLE=worker|off") {
		t.Fatalf("validateOwnership() error = %v, want scheduler role enumeration requirement", err)
	}
}

func TestValidateAlarmWorkerOwnershipProductionRejectsNonOwnerEgressRoles(t *testing.T) {
	for _, role := range []string{load.NotificationEgressRoleProducer, load.NotificationEgressRoleOff} {
		t.Run(role, func(t *testing.T) {
			settingstest.ClearRuntimeRoleEnv(t)
			t.Setenv(load.NotificationEgressRoleEnv, role)
			t.Setenv(load.NotificationSchedulerRoleEnv, load.NotificationSchedulerRoleWorker)

			err := validateOwnership(load.EnvironmentProduction)
			if err == nil || !strings.Contains(err.Error(), "alarm-worker production requires NOTIFICATION_EGRESS_ROLE=owner") {
				t.Fatalf("validateOwnership() error = %v, want owner role requirement", err)
			}
		})
	}
}

func TestValidateAlarmWorkerOwnershipNonProductionSkipsOwnershipRequirements(t *testing.T) {
	settingstest.ClearRuntimeRoleEnv(t)

	if err := validateOwnership("staging"); err != nil {
		t.Fatalf("validateOwnership() error = %v, want nil", err)
	}
}

func TestValidateAlarmWorkerOwnershipProductionAcceptsSchedulerWorkerProfile(t *testing.T) {
	settingstest.ClearRuntimeRoleEnv(t)
	t.Setenv(load.NotificationEgressRoleEnv, load.NotificationEgressRoleOwner)
	t.Setenv(load.NotificationSchedulerRoleEnv, load.NotificationSchedulerRoleWorker)

	if err := validateOwnership(load.EnvironmentProduction); err != nil {
		t.Fatalf("validateOwnership() error = %v, want nil", err)
	}
}

func TestValidateAlarmWorkerOwnershipProductionAcceptsEgressOnlyProfile(t *testing.T) {
	settingstest.ClearRuntimeRoleEnv(t)
	t.Setenv(load.NotificationEgressRoleEnv, load.NotificationEgressRoleOwner)
	t.Setenv(load.NotificationSchedulerRoleEnv, load.NotificationSchedulerRoleOff)

	if err := validateOwnership(load.EnvironmentProduction); err != nil {
		t.Fatalf("validateOwnership() error = %v, want nil", err)
	}
}

func TestValidateAlarmWorkerOwnershipRejectsUnsupportedSchedulerRole(t *testing.T) {
	settingstest.ClearRuntimeRoleEnv(t)
	t.Setenv(load.NotificationEgressRoleEnv, load.NotificationEgressRoleOwner)
	t.Setenv(load.NotificationSchedulerRoleEnv, "bot")

	err := validateOwnership(load.EnvironmentProduction)
	if err == nil || !strings.Contains(err.Error(), "unsupported NOTIFICATION_SCHEDULER_ROLE=bot") {
		t.Fatalf("validateOwnership() error = %v, want unsupported scheduler role rejection", err)
	}
}

func TestValidateProductionAlarmExecutorsRejectsDisabledProfileExecutor(t *testing.T) {
	for _, workerID := range []string{"alarm_dispatch", "notification_delivery", "youtube_delivery"} {
		t.Run(workerID, func(t *testing.T) {
			profile := alarmWorkerProfileFixture(t)
			worker := profile.Loaded.Profile.Workers[workerID]

			worker.Executor.Enabled = false
			profile.Loaded.Profile.Workers[workerID] = worker

			err := validateProductionExecutors(profile)
			if err == nil || !strings.Contains(err.Error(), "requires "+workerID+" executor.enabled=true") {
				t.Fatalf("validateProductionExecutors() error = %v, want %s profile requirement", err, workerID)
			}
		})
	}
}

func TestValidateProductionAlarmExecutorsAcceptsFixtureProfile(t *testing.T) {
	if err := validateProductionExecutors(alarmWorkerProfileFixture(t)); err != nil {
		t.Fatalf("validateProductionExecutors() error = %v, want nil", err)
	}
}
