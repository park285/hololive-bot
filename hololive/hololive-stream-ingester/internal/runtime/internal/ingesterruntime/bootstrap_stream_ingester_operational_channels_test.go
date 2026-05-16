package ingesterruntime

import (
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/config"
)

func TestBuildStreamIngesterRuntime_FailsWhenOperationalChannelRepositoryUnavailable(t *testing.T) {
	originalInit := initStreamIngesterInfrastructureFn
	t.Cleanup(func() {
		initStreamIngesterInfrastructureFn = originalInit
	})

	cleanupCalls := 0
	initStreamIngesterInfrastructureFn = func(context.Context, *config.Config, *slog.Logger) (*streamIngesterInfrastructure, error) {
		return &streamIngesterInfrastructure{
			cleanup: func() { cleanupCalls++ },
		}, nil
	}

	runtime, err := BuildStreamIngesterRuntime(context.Background(), &config.Config{
		Server: config.ServerConfig{Port: 30004},
		Ingestion: config.IngestionConfig{
			YouTubeEnabled: true,
		},
	}, testLogger())
	require.Error(t, err)
	require.Nil(t, runtime)
	require.ErrorContains(t, err, "resolve community shorts operational channels")
	require.ErrorContains(t, err, "member repository is nil")
	assert.Equal(t, 1, cleanupCalls)
}
