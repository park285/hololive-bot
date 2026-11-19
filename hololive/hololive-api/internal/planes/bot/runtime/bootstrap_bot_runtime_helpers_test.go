package botruntime

import (
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	apiconfig "github.com/kapu/hololive-api/internal/config"
)

func TestBuildBotRuntime_RejectsNilAppConfig(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.DiscardHandler)
	runtime, err := buildBotRuntime(t.Context(), nil, logger, nil)
	require.Error(t, err)
	assert.Nil(t, runtime)
	assert.Contains(t, err.Error(), "app config is nil")
}

func TestBuildBotRuntime_RejectsNilInfrastructure(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.DiscardHandler)
	runtime, err := buildBotRuntime(t.Context(), &apiconfig.BotPlaneConfig{}, logger, nil)
	require.Error(t, err)
	assert.Nil(t, runtime)
	assert.Contains(t, err.Error(), "infra is nil")
}
