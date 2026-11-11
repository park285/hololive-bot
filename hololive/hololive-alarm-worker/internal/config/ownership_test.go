package config

import (
	"strings"
	"testing"

	"github.com/kapu/hololive-shared/pkg/config/runtimepolicy"
	"github.com/kapu/hololive-shared/pkg/config/settingstest"
)

func alarmWorkerProfileFixture(t *testing.T) *AlarmWorkerProfile {
	t.Helper()
	settingstest.UseProfileFixture(t, "testdata/stack-worker-profile-alarm-worker.json")

	profile, err := LoadWorkerProfile()
	if err != nil {
		t.Fatalf("load alarm worker profile fixture: %v", err)
	}

	return profile
}

func TestValidateAlarmWorkerOwnershipProductionRequiresOwnerWorkerRoles(t *testing.T) {
	settingstest.ClearRuntimeRoleEnv(t)

	err := validateOwnership(runtimepolicy.EnvironmentProduction, loadNotificationRoles())
	if err == nil || !strings.Contains(err.Error(), "alarm-worker production requires NOTIFICATION_EGRESS_ROLE=owner") {
		t.Fatalf("validateOwnership() error = %v, want owner role requirement", err)
	}

	settingstest.ClearRuntimeRoleEnv(t)
	t.Setenv(runtimepolicy.NotificationEgressRoleEnv, runtimepolicy.NotificationEgressRoleOwner)

	err = validateOwnership(runtimepolicy.EnvironmentProduction, loadNotificationRoles())
	if err == nil || !strings.Contains(err.Error(), "alarm-worker production requires NOTIFICATION_SCHEDULER_ROLE=worker|off") {
		t.Fatalf("validateOwnership() error = %v, want scheduler role enumeration requirement", err)
	}
}

func TestValidateAlarmWorkerOwnershipProductionRejectsNonOwnerEgressRoles(t *testing.T) {
	for _, role := range []string{runtimepolicy.NotificationEgressRoleProducer, runtimepolicy.NotificationEgressRoleOff} {
		t.Run(role, func(t *testing.T) {
			settingstest.ClearRuntimeRoleEnv(t)
			t.Setenv(runtimepolicy.NotificationEgressRoleEnv, role)
			t.Setenv(runtimepolicy.NotificationSchedulerRoleEnv, runtimepolicy.NotificationSchedulerRoleWorker)

			err := validateOwnership(runtimepolicy.EnvironmentProduction, loadNotificationRoles())
			if err == nil || !strings.Contains(err.Error(), "alarm-worker production requires NOTIFICATION_EGRESS_ROLE=owner") {
				t.Fatalf("validateOwnership() error = %v, want owner role requirement", err)
			}
		})
	}
}

func TestValidateAlarmWorkerOwnershipNonProductionSkipsOwnershipRequirements(t *testing.T) {
	settingstest.ClearRuntimeRoleEnv(t)

	if err := validateOwnership("staging", loadNotificationRoles()); err != nil {
		t.Fatalf("validateOwnership() error = %v, want nil", err)
	}
}

func TestValidateAlarmWorkerOwnershipProductionAcceptsSchedulerWorkerProfile(t *testing.T) {
	settingstest.ClearRuntimeRoleEnv(t)
	t.Setenv(runtimepolicy.NotificationEgressRoleEnv, runtimepolicy.NotificationEgressRoleOwner)
	t.Setenv(runtimepolicy.NotificationSchedulerRoleEnv, runtimepolicy.NotificationSchedulerRoleWorker)

	if err := validateOwnership(runtimepolicy.EnvironmentProduction, loadNotificationRoles()); err != nil {
		t.Fatalf("validateOwnership() error = %v, want nil", err)
	}
}

func TestValidateAlarmWorkerOwnershipProductionAcceptsEgressOnlyProfile(t *testing.T) {
	settingstest.ClearRuntimeRoleEnv(t)
	t.Setenv(runtimepolicy.NotificationEgressRoleEnv, runtimepolicy.NotificationEgressRoleOwner)
	t.Setenv(runtimepolicy.NotificationSchedulerRoleEnv, runtimepolicy.NotificationSchedulerRoleOff)

	if err := validateOwnership(runtimepolicy.EnvironmentProduction, loadNotificationRoles()); err != nil {
		t.Fatalf("validateOwnership() error = %v, want nil", err)
	}
}

func TestValidateAlarmWorkerOwnershipRejectsUnsupportedSchedulerRole(t *testing.T) {
	settingstest.ClearRuntimeRoleEnv(t)
	t.Setenv(runtimepolicy.NotificationEgressRoleEnv, runtimepolicy.NotificationEgressRoleOwner)
	t.Setenv(runtimepolicy.NotificationSchedulerRoleEnv, "bot")

	err := validateOwnership(runtimepolicy.EnvironmentProduction, loadNotificationRoles())
	if err == nil || !strings.Contains(err.Error(), "unsupported NOTIFICATION_SCHEDULER_ROLE=bot") {
		t.Fatalf("validateOwnership() error = %v, want unsupported scheduler role rejection", err)
	}
}

// LoadRuntime은 알림 역할 env의 앞뒤 공백을 지워 설정에 담는다.
func TestLoadRuntimeReadsNotificationRoles(t *testing.T) {
	setRuntimeEnv(t)
	t.Setenv(runtimepolicy.NotificationEgressRoleEnv, " owner ")
	t.Setenv(runtimepolicy.NotificationSchedulerRoleEnv, "off")

	config, err := LoadRuntime()
	if err != nil {
		t.Fatalf("LoadRuntime() error = %v", err)
	}

	if want := (NotificationRoles{Egress: "owner", Scheduler: "off"}); config.NotificationRoles != want {
		t.Fatalf("NotificationRoles = %+v, want %+v", config.NotificationRoles, want)
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
