package app

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/service/cache"
)

func TestBuildBotRuntimeIngestion(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	t.Run("disabled ingestion returns zero components", func(t *testing.T) {
		cfg := &config.Config{
			Bot: config.BotConfig{
				IngestionEnabled: false,
			},
		}

		components, err := buildBotRuntimeIngestion(context.Background(), cfg, botRuntimeDependencyViews{}, logger)
		if err != nil {
			t.Fatalf("buildBotRuntimeIngestion() error = %v", err)
		}
		if components.scheduler != nil || components.scraperScheduler != nil || components.photoSyncService != nil || components.outboxDispatcher != nil || components.ingestionLease != nil {
			t.Fatal("disabled ingestion must yield zero-value runtime ingestion components")
		}
	})

	t.Run("enabled ingestion fails fast when dependency view is incomplete", func(t *testing.T) {
		cfg := &config.Config{
			Bot: config.BotConfig{
				IngestionEnabled: true,
			},
		}

		components, err := buildBotRuntimeIngestion(context.Background(), cfg, botRuntimeDependencyViews{
			ingestion: botIngestionRuntimeDependencies{
				cache: &cache.Service{},
			},
		}, logger)
		if err == nil {
			t.Fatal("buildBotRuntimeIngestion() error = nil, want non-nil")
		}
		if !strings.Contains(err.Error(), "dependency view is incomplete") {
			t.Fatalf("buildBotRuntimeIngestion() error = %v, want dependency view is incomplete", err)
		}
		if components.ingestionLease != nil || components.scraperScheduler != nil || components.outboxDispatcher != nil {
			t.Fatal("failed ingestion build must not produce runtime components")
		}
	})
}
