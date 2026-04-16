package app

import (
	"log/slog"
	"testing"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildAlarmWorkerRuntime_FailFastOnNilInputs(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.DiscardHandler)

	runtime, err := BuildAlarmWorkerRuntime(t.Context(), nil, logger)
	require.Error(t, err)
	assert.Nil(t, runtime)
	assert.Equal(t, "config must not be nil", err.Error())

	runtime, err = BuildAlarmWorkerRuntime(t.Context(), &config.Config{}, nil)
	require.Error(t, err)
	assert.Nil(t, runtime)
	assert.Equal(t, "logger must not be nil", err.Error())
}

func TestRuntimeAllowsAlarmScheduler(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		runtimeRole string
		configValue string
		want        bool
	}{
		{name: "default bot role", runtimeRole: "bot", configValue: "", want: true},
		{name: "default worker role", runtimeRole: "worker", configValue: "", want: true},
		{name: "bot explicitly enabled", runtimeRole: "bot", configValue: "bot", want: true},
		{name: "worker explicitly enabled", runtimeRole: "worker", configValue: "worker", want: true},
		{name: "bot disabled when worker owns scheduler", runtimeRole: "bot", configValue: "worker", want: false},
		{name: "worker disabled when bot owns scheduler", runtimeRole: "worker", configValue: "bot", want: false},
		{name: "off disables all", runtimeRole: "bot", configValue: "off", want: false},
		{name: "unknown disables", runtimeRole: "worker", configValue: "mystery", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, runtimeAllowsAlarmScheduler(tt.runtimeRole, tt.configValue))
		})
	}
}
