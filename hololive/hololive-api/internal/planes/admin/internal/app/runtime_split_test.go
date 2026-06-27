package app

import (
	"log/slog"
	"testing"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildAdminAPIRuntime_FailFastOnNilInputs(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.DiscardHandler)

	runtime, err := BuildAdminAPIRuntime(t.Context(), nil, logger)
	require.Error(t, err)
	assert.Nil(t, runtime)
	assert.Equal(t, "config must not be nil", err.Error())

	runtime, err = BuildAdminAPIRuntime(t.Context(), &config.Config{}, nil)
	require.Error(t, err)
	assert.Nil(t, runtime)
	assert.Equal(t, "logger must not be nil", err.Error())
}
