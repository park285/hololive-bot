package workerapp

import (
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	workerconfig "github.com/kapu/hololive-alarm-worker/internal/config"
	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/subscriptions"
)

func TestBuildAlarmWorkerRuntime_FailFastOnNilInputs(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.DiscardHandler)

	runtime, err := BuildAlarmWorkerRuntime(t.Context(), nil, logger)
	require.Error(t, err)
	assert.Nil(t, runtime)
	assert.Equal(t, "normalize runtime build inputs: config must not be nil", err.Error())

	runtime, err = BuildAlarmWorkerRuntime(t.Context(), &workerconfig.RuntimeConfig{}, nil)
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

	appConfig, err := loadAlarmDispatchPublishConfig(&workerconfig.AlarmWorkerProfile{
		AlarmDispatch: workerconfig.AlarmDispatchWorkerSettings{WakeupEnabled: true},
	})
	require.NoError(t, err)
	assert.True(t, appConfig.WakeupEnabled)
	assert.Equal(t, 1000, appConfig.MaxDeliveriesPerBatch)
}

// 잘못된 batch 한도는 기본값 1000으로 바뀌지 않고 설정 오류가 된다(holo-alarm-worker-envconfig-silent-defaults).
func TestLoadAlarmDispatchPublishConfigRejectsInvalidMaxDeliveries(t *testing.T) {
	for _, value := range []string{"many", "0", "-10"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("ALARM_DISPATCH_MAX_DELIVERIES_PER_BATCH", value)

			_, err := loadAlarmDispatchPublishConfig(&workerconfig.AlarmWorkerProfile{})
			require.ErrorContains(t, err, "ALARM_DISPATCH_MAX_DELIVERIES_PER_BATCH")
		})
	}
}

func TestRuntimeSchedulerRejectsMissingServiceBeforeInterfaceConversion(t *testing.T) {
	_, err := buildRuntimeScheduler(&workerconfig.RuntimeConfig{}, nil, &alarmFoundation{}, nil)
	require.ErrorContains(t, err, "alarm service is required")
}

func TestRuntimeSchedulerDisabledSkipsDependencyConstruction(t *testing.T) {
	t.Setenv(notificationSchedulerRoleEnv, schedulerRoleOff)

	result := buildOptionalRuntimeScheduler(nil, nil, nil, nil)
	require.NoError(t, result.err)
	require.Nil(t, result.scheduler)
}

func TestRuntimeSchedulerRejectsMissingInfrastructure(t *testing.T) {
	config := &workerconfig.RuntimeConfig{AlarmWorkerProfile: &workerconfig.AlarmWorkerProfile{}}
	_, err := buildRuntimeScheduler(config, nil, &alarmFoundation{AlarmService: &subscriptions.AlarmService{}}, nil)
	require.ErrorContains(t, err, "infrastructure is required")
}
