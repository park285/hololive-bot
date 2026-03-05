package app

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/constants"
)

func TestDBIntegrationRuntimeClose_CallsCleanupOnce(t *testing.T) {
	t.Parallel()

	calls := 0
	runtime := &DBIntegrationRuntime{
		cleanup: func() { calls++ },
	}

	runtime.Close()
	assert.Equal(t, 1, calls)

	var nilRuntime *DBIntegrationRuntime
	require.NotPanics(t, func() {
		nilRuntime.Close()
	})
}

func TestBuildDBIntegrationRuntime_ReturnsErrorOnNilLogger(t *testing.T) {
	t.Parallel()

	runtime, err := BuildDBIntegrationRuntime(context.Background(), config.PostgresConfig{}, nil)
	require.Error(t, err)
	assert.Nil(t, runtime)
	assert.Contains(t, err.Error(), "logger must not be nil")
}

func TestFetchProfilesRuntimeClose_CallsCleanupOnce(t *testing.T) {
	t.Parallel()

	calls := 0
	runtime := &FetchProfilesRuntime{
		cleanup: func() { calls++ },
	}

	runtime.Close()
	assert.Equal(t, 1, calls)

	var nilRuntime *FetchProfilesRuntime
	require.NotPanics(t, func() {
		nilRuntime.Close()
	})
}

func TestBuildFetchProfilesRuntime_WithNilContext(t *testing.T) {
	t.Parallel()

	runtime, err := BuildFetchProfilesRuntime(nil)
	require.NoError(t, err)
	require.NotNil(t, runtime)
	require.NotNil(t, runtime.Logger)
	require.NotNil(t, runtime.HTTPClient)
	assert.Equal(t, constants.OfficialProfileConfig.RequestTimeout, runtime.HTTPClient.Timeout)

	runtime.Close()
}

func TestBuildDBIntegrationRuntime_InitializesContextWhenNil(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	runtime, err := BuildDBIntegrationRuntime(nil, config.PostgresConfig{}, logger)
	require.Error(t, err)
	assert.Nil(t, runtime)
	assert.Contains(t, err.Error(), "failed to initialize DB integration runtime")
}
