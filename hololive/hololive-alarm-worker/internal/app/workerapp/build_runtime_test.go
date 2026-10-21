package workerapp

import (
	"log/slog"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	workerconfig "github.com/kapu/hololive-alarm-worker/internal/config"
	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/queue"
	sharedmodules "github.com/kapu/hololive-shared/pkg/providers/modules"
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

func TestAlarmSchedulerEnabled(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		configValue string
		want        bool
	}{
		{name: "default worker role", configValue: "", want: true},
		{name: "worker explicitly enabled", configValue: " Worker ", want: true},
		{name: "off disables", configValue: "off", want: false},
		{name: "retired bot role disables", configValue: "bot", want: false},
		{name: "unknown disables", configValue: "mystery", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, alarmSchedulerEnabled(tt.configValue))
		})
	}
}

func TestLoadAlarmDispatchPublishConfigDefaults(t *testing.T) {
	t.Setenv("ALARM_DISPATCH_MAX_DELIVERIES_PER_BATCH", "1000")
	require.NoError(t, os.Unsetenv("ALARM_DISPATCH_MAX_DELIVERIES_PER_BATCH"))

	appConfig, err := loadAlarmDispatchPublishConfig(&workerconfig.AlarmWorkerProfile{
		AlarmDispatch: workerconfig.AlarmDispatchWorkerSettings{WakeupEnabled: true},
	})
	require.NoError(t, err)
	assert.True(t, appConfig.WakeupEnabled)
	assert.Equal(t, 1000, appConfig.MaxDeliveriesPerBatch)
}

// 잘못된 batch 한도는 기본값 1000으로 바뀌지 않고 설정 오류가 된다. 비어 있거나 공백뿐인 값도 strict env 계약대로 오류다.
func TestLoadAlarmDispatchPublishConfigRejectsInvalidMaxDeliveries(t *testing.T) {
	for _, value := range []string{"many", "0", "-10", "", "  "} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("ALARM_DISPATCH_MAX_DELIVERIES_PER_BATCH", value)

			_, err := loadAlarmDispatchPublishConfig(&workerconfig.AlarmWorkerProfile{})
			require.ErrorContains(t, err, "ALARM_DISPATCH_MAX_DELIVERIES_PER_BATCH")
		})
	}
}

func TestRuntimeSchedulerDisabledSkipsDependencyConstruction(t *testing.T) {
	t.Setenv(notificationSchedulerRoleEnv, "off")

	result := buildOptionalRuntimeScheduler(
		&workerconfig.RuntimeConfig{}, &sharedmodules.InfraModule{}, &alarmFoundation{}, queue.PublishConfig{}, slog.New(slog.DiscardHandler),
	)
	require.NoError(t, result.err)
	require.Nil(t, result.scheduler)
}
