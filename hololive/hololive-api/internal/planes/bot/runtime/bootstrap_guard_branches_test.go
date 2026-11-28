package botruntime

import (
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	apiconfig "github.com/kapu/hololive-api/internal/config"
	appbootstrap "github.com/kapu/hololive-api/internal/planes/bot/internal/app/bootstrap"
)

func testBootstrapGuardLogger() *slog.Logger { return slog.New(slog.DiscardHandler) }
func canceledContext() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	return ctx
}

func TestInitBotInfrastructureRequiresProcessResources(t *testing.T) {
	t.Parallel()

	infra, err := appbootstrap.InitBotInfrastructure(canceledContext(), &apiconfig.BotPlaneConfig{}, nil, appbootstrap.PlaneDependencies{}, testBootstrapGuardLogger())
	require.Error(t, err)
	assert.Nil(t, infra)
	assert.ErrorContains(t, err, "shared infra must not be nil")
}

func TestBuildRuntimeRequiresProcessResources(t *testing.T) {
	t.Parallel()

	runtime, err := BuildRuntime(canceledContext(), &apiconfig.BotPlaneConfig{}, nil, Dependencies{}, testBootstrapGuardLogger())
	require.Error(t, err)
	assert.Nil(t, runtime)
	assert.Contains(t, err.Error(), "shared infra must not be nil")
}

func TestBuildBotRuntime_FailsFastWhenBotDependenciesMissing(t *testing.T) {
	t.Parallel()

	runtime, err := buildBotRuntime(t.Context(), &apiconfig.BotPlaneConfig{}, testBootstrapGuardLogger(), &appbootstrap.BotInfrastructure{})
	require.Error(t, err)
	assert.Nil(t, runtime)
	assert.Contains(t, err.Error(), "durable store is nil")
}
