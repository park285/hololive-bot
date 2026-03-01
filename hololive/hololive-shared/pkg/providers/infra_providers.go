package providers

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/iris"
	"github.com/kapu/hololive-shared/pkg/platform/bootstrap"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/database"
	"github.com/kapu/hololive-shared/pkg/service/settings"
	"github.com/kapu/hololive-shared/pkg/service/template"
)

// ProvideValkeyConfig - 설정에서 Valkey 캐시 설정 추출
func ProvideValkeyConfig(cfg *config.Config) config.ValkeyConfig {
	return cfg.Valkey
}

// ProvidePostgresConfig - 설정에서 PostgreSQL 설정 추출
func ProvidePostgresConfig(cfg *config.Config) config.PostgresConfig {
	return cfg.Postgres
}

// ProvideCacheResources - 캐시 리소스 생성 (정리 함수 포함)
func ProvideCacheResources(ctx context.Context, cfg config.ValkeyConfig, logger *slog.Logger) (*bootstrap.CacheResources, func(), error) {
	resources, err := bootstrap.NewCacheResources(ctx, cfg, logger)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create cache resources: %w", err)
	}
	return resources, resources.Close, nil
}

// ProvideCacheService - 캐시 리소스에서 서비스 추출
func ProvideCacheService(resources *bootstrap.CacheResources) *cache.Service {
	return resources.Service
}

// ProvideDatabaseResources - 데이터베이스 리소스 생성 (정리 함수 포함)
func ProvideDatabaseResources(ctx context.Context, cfg config.PostgresConfig, logger *slog.Logger) (*bootstrap.DatabaseResources, func(), error) {
	resources, err := bootstrap.NewDatabaseResources(ctx, cfg, logger)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create database resources: %w", err)
	}
	return resources, resources.Close, nil
}

// ProvidePostgresService - 데이터베이스 리소스에서 서비스 추출
func ProvidePostgresService(resources *bootstrap.DatabaseResources) *database.PostgresService {
	return resources.Service
}

// ProvideIrisClient - Iris h2c(HTTP/2 Cleartext) 클라이언트 생성
func ProvideIrisClient(cfg config.IrisConfig, logger *slog.Logger) iris.Client {
	return iris.NewH2CClient(cfg.BaseURL, cfg.BotToken, logger, iris.H2CClientOptions{
		Timeout:               cfg.HTTPTimeout,
		DialTimeout:           cfg.HTTPDialTimeout,
		ResponseHeaderTimeout: cfg.HTTPResponseHeaderTimeout,
	})
}

// ProvideSettingsService - 설정 서비스 생성
func ProvideSettingsService(advanceMinutes []int, scraperProxyEnabled bool, logger *slog.Logger) *settings.Service {
	settingsPath := resolveSettingsFilePath()
	if logger != nil {
		logger.Info("Using settings file path", slog.String("path", settingsPath))
	}

	return settings.NewSettingsService(settingsPath, settings.Settings{
		AlarmAdvanceMinutes: defaultAlarmAdvanceMinute(advanceMinutes),
		ScraperProxyEnabled: scraperProxyEnabled,
	}, logger)
}

// ProvideMessageStack - 메시지 어댑터 및 포매터 생성
func ProvideMessageStack(botPrefix string, renderer *template.Renderer) *MessageStack {
	msgAdapter, formatter := bootstrap.NewMessageStack(botPrefix, renderer)
	return &MessageStack{
		Adapter:   msgAdapter,
		Formatter: formatter,
	}
}
