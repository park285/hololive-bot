package app

import (
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/config/settings"
)

func TestBuildAdminAPIRuntime_FailFastOnNilInputs(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.DiscardHandler)

	runtime, err := BuildAdminAPIRuntime(t.Context(), nil, logger)
	require.Error(t, err)
	assert.Nil(t, runtime)
	require.ErrorContains(t, err, "config must not be nil")

	runtime, err = BuildAdminAPIRuntime(t.Context(), &settings.Config{}, nil)
	require.Error(t, err)
	assert.Nil(t, runtime)
	require.ErrorContains(t, err, "logger must not be nil")
}

func TestNormalizeAdminAPIRuntimeInputs_ReturnsValidatedConfig(t *testing.T) {
	t.Parallel()

	inputCtx := t.Context()
	appConfig := &settings.Config{}
	logger := slog.New(slog.DiscardHandler)

	ctx, validatedConfig, err := normalizeAdminAPIRuntimeInputs(inputCtx, appConfig, logger)
	require.NoError(t, err)
	assert.Equal(t, inputCtx, ctx)
	assert.Same(t, appConfig, validatedConfig)
}
