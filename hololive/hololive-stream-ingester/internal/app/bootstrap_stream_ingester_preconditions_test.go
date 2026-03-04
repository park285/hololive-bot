package app

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/config"
)

func newStreamIngesterTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestBuildStreamIngesterRuntime_Preconditions(t *testing.T) {
	t.Run("nil config", func(t *testing.T) {
		runtime, err := BuildStreamIngesterRuntime(context.Background(), nil, newStreamIngesterTestLogger())
		require.Error(t, err)
		assert.Nil(t, runtime)
		assert.Equal(t, "config must not be nil", err.Error())
	})

	t.Run("nil logger", func(t *testing.T) {
		cfg := &config.Config{}
		runtime, err := BuildStreamIngesterRuntime(context.Background(), cfg, nil)
		require.Error(t, err)
		assert.Nil(t, runtime)
		assert.Equal(t, "logger must not be nil", err.Error())
	})

	t.Run("ingestion disabled", func(t *testing.T) {
		cfg := &config.Config{
			Bot: config.BotConfig{IngestionEnabled: false},
		}
		runtime, err := BuildStreamIngesterRuntime(context.Background(), cfg, newStreamIngesterTestLogger())
		require.Error(t, err)
		assert.Nil(t, runtime)
		assert.Equal(t, "stream ingester requires BOT_INGESTION_ENABLED=true", err.Error())
	})
}
