package workerapp

import (
	"log/slog"
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
	assert.Equal(t, "normalize runtime build inputs: bootstrap: config must not be nil", err.Error())
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

func TestRuntimeSchedulerDisabledSkipsDependencyConstruction(t *testing.T) {
	t.Setenv(notificationSchedulerRoleEnv, "off")

	result := buildOptionalRuntimeScheduler(
		&workerconfig.RuntimeConfig{}, &sharedmodules.InfraModule{}, &alarmFoundation{}, queue.PublishConfig{}, slog.New(slog.DiscardHandler),
	)
	require.NoError(t, result.err)
	require.Nil(t, result.scheduler)
}
