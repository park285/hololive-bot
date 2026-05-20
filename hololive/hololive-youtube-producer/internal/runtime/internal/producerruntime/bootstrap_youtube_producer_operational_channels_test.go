package producerruntime

import (
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/config"
)

func TestBuildYouTubeProducerRuntime_FailsWhenOperationalChannelRepositoryUnavailable(t *testing.T) {
	t.Setenv("YOUTUBE_PRODUCER_RUNTIME_ALLOWED", "true")

	originalInit := initYouTubeProducerInfrastructureFn
	t.Cleanup(func() {
		initYouTubeProducerInfrastructureFn = originalInit
	})

	cleanupCalls := 0
	initYouTubeProducerInfrastructureFn = func(context.Context, *config.Config, *slog.Logger) (*youtubeProducerInfrastructure, error) {
		return &youtubeProducerInfrastructure{
			cleanup: func() { cleanupCalls++ },
		}, nil
	}

	runtime, err := BuildYouTubeProducerRuntime(context.Background(), &config.Config{
		Server: config.ServerConfig{Port: 30005},
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
