package workerapp

import (
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/config/settings"
	"github.com/kapu/hololive-shared/pkg/config/settings/alarmworker"
	"github.com/kapu/hololive-shared/pkg/service/notification/alarmservice"
)

func TestBuildAlarmWorkerRuntime_FailFastOnNilInputs(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.DiscardHandler)

	runtime, err := BuildAlarmWorkerRuntime(t.Context(), nil, logger)
	require.Error(t, err)
	assert.Nil(t, runtime)
	assert.Equal(t, "normalize runtime build inputs: config must not be nil", err.Error())

	runtime, err = BuildAlarmWorkerRuntime(t.Context(), &alarmworker.RuntimeConfig{}, nil)
	require.Error(t, err)
	assert.Nil(t, runtime)
	assert.Equal(t, "normalize runtime build inputs: logger must not be nil", err.Error())
}

func TestRuntimeAllowsAlarmScheduler(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		runtimeRole string
		configValue string
		want        bool
	}{
		{name: "default bot role", runtimeRole: runtimeRoleBot, configValue: "", want: true},
		{name: "default worker role", runtimeRole: runtimeRoleWorker, configValue: "", want: true},
		{name: "bot explicitly enabled", runtimeRole: runtimeRoleBot, configValue: runtimeRoleBot, want: true},
		{name: "worker explicitly enabled", runtimeRole: runtimeRoleWorker, configValue: runtimeRoleWorker, want: true},
		{name: "bot disabled when worker owns scheduler", runtimeRole: runtimeRoleBot, configValue: runtimeRoleWorker, want: false},
		{name: "worker disabled when bot owns scheduler", runtimeRole: runtimeRoleWorker, configValue: runtimeRoleBot, want: false},
		{name: "off disables all", runtimeRole: runtimeRoleBot, configValue: schedulerRoleOff, want: false},
		{name: "unknown disables", runtimeRole: runtimeRoleWorker, configValue: "mystery", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, runtimeAllowsAlarmScheduler(tt.runtimeRole, tt.configValue))
		})
	}
}

func TestLoadAlarmDispatchPublishConfigDefaults(t *testing.T) {
	t.Setenv("ALARM_DISPATCH_MAX_DELIVERIES_PER_BATCH", "")

	appConfig := loadAlarmDispatchPublishConfig(&settings.AlarmWorkerProfile{
		AlarmDispatch: settings.AlarmDispatchWorkerSettings{WakeupEnabled: true},
	})
	assert.True(t, appConfig.WakeupEnabled)
	assert.Equal(t, 1000, appConfig.MaxDeliveriesPerBatch)
}

func TestRuntimeSchedulerRejectsMissingServiceBeforeInterfaceConversion(t *testing.T) {
	_, err := buildRuntimeScheduler(&settings.Config{}, nil, &alarmFoundation{}, nil)
	require.ErrorContains(t, err, "alarm service is required")
}

func TestRuntimeSchedulerDisabledSkipsDependencyConstruction(t *testing.T) {
	t.Setenv(notificationSchedulerRoleEnv, schedulerRoleOff)

	result := buildOptionalRuntimeScheduler(nil, nil, nil, nil)
	require.NoError(t, result.err)
	require.Nil(t, result.scheduler)
}

func TestRuntimeSchedulerRejectsMissingInfrastructure(t *testing.T) {
	config := &settings.Config{AlarmWorkerProfile: &settings.AlarmWorkerProfile{}}
	_, err := buildRuntimeScheduler(config, nil, &alarmFoundation{AlarmService: &alarmservice.AlarmService{}}, nil)
	require.ErrorContains(t, err, "infrastructure is required")
}
