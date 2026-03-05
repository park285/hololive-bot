package app

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
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

	t.Run("nil config fails fast", func(t *testing.T) {
		components, err := buildBotRuntimeIngestion(context.Background(), nil, botRuntimeDependencyViews{}, logger)
		if err == nil {
			t.Fatal("buildBotRuntimeIngestion() error = nil, want non-nil")
		}
		if !strings.Contains(err.Error(), "config is nil") {
			t.Fatalf("buildBotRuntimeIngestion() error = %v, want config is nil", err)
		}
		if components.ingestionLease != nil || components.scraperScheduler != nil || components.outboxDispatcher != nil {
			t.Fatal("nil config must not produce runtime components")
		}
	})

	t.Run("enabled ingestion wraps lease acquisition error", func(t *testing.T) {
		cfg := &config.Config{
			Bot: config.BotConfig{
				IngestionEnabled: true,
			},
		}
		leaseErr := errors.New("setnx failed")
		cacheSvc := &cachemocks.Client{
			SetNXFunc: func(context.Context, string, string, time.Duration) (bool, error) {
				return false, leaseErr
			},
		}
		settingsSvc := &trackingSettingsReadWriter{}

		components, err := buildBotRuntimeIngestion(context.Background(), cfg, botRuntimeDependencyViews{
			ingestion: botIngestionRuntimeDependencies{
				cache:      cacheSvc,
				postgres:   &nilGormPostgres{},
				irisClient: &stubIrisClient{},
				members:    &stubMemberDataProvider{},
				settings:   settingsSvc,
			},
		}, logger)
		if err == nil {
			t.Fatal("buildBotRuntimeIngestion() error = nil, want non-nil")
		}
		if !strings.Contains(err.Error(), "acquire ingestion lease") {
			t.Fatalf("buildBotRuntimeIngestion() error = %v, want acquire ingestion lease", err)
		}
		if !strings.Contains(err.Error(), leaseErr.Error()) {
			t.Fatalf("buildBotRuntimeIngestion() error = %v, want wrapped setnx error", err)
		}
		if components.ingestionLease != nil || components.scraperScheduler != nil || components.outboxDispatcher != nil {
			t.Fatal("lease acquisition failure must not produce runtime components")
		}
	})
}
