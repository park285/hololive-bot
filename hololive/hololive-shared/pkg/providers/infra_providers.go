// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package providers

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/database"
	"github.com/kapu/hololive-shared/pkg/service/settings"
	iris "github.com/park285/iris-client-go/client"
)

// CacheResources: 초기화된 캐시 서비스 인스턴스와 리소스 해제(Close) 함수를 캡슐화한 구조체
type CacheResources struct {
	Service *cache.Service
	Close   func()
}

// DatabaseResources: 초기화된 DB 서비스 인스턴스와 리소스 해제(Close) 함수를 캡슐화한 구조체
type DatabaseResources struct {
	Service *database.PostgresService
	Close   func()
}

// ProvideValkeyConfig - 설정에서 Valkey 캐시 설정 추출
func ProvideValkeyConfig(cfg *config.Config) config.ValkeyConfig {
	return cfg.Valkey
}

// ProvidePostgresConfig - 설정에서 PostgreSQL 설정 추출
func ProvidePostgresConfig(cfg *config.Config) config.PostgresConfig {
	return cfg.Postgres
}

// ProvideCacheResources - 캐시 리소스 생성 (정리 함수 포함)
func ProvideCacheResources(ctx context.Context, cfg config.ValkeyConfig, logger *slog.Logger) (*CacheResources, func(), error) {
	cacheSvc, err := cache.NewCacheService(ctx, cache.Config{
		Host:       cfg.Host,
		Port:       cfg.Port,
		Password:   cfg.Password,
		DB:         cfg.DB,
		SocketPath: cfg.SocketPath,
	}, logger)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create cache resources: %w", err)
	}

	resources := &CacheResources{
		Service: cacheSvc,
		Close: func() {
			_ = cacheSvc.Close()
		},
	}
	return resources, resources.Close, nil
}

// ProvideCacheService - 캐시 리소스에서 서비스 추출
func ProvideCacheService(resources *CacheResources) cache.Client {
	return resources.Service
}

// ProvideDatabaseResources - 데이터베이스 리소스 생성 (정리 함수 포함)
func ProvideDatabaseResources(ctx context.Context, cfg config.PostgresConfig, logger *slog.Logger) (*DatabaseResources, func(), error) {
	dbSvc, err := database.NewPostgresService(ctx, database.PostgresConfig{
		Host:             cfg.Host,
		Port:             cfg.Port,
		SocketPath:       cfg.SocketPath,
		User:             cfg.User,
		Password:         cfg.Password,
		Database:         cfg.Database,
		SSLMode:          cfg.SSLMode,
		QueryExecMode:    cfg.QueryExecMode,
		PoolMinConns:     cfg.PoolMinConns,
		PoolMaxConns:     cfg.PoolMaxConns,
		PoolMaxIdleConns: cfg.PoolMaxIdleConns,
	}, logger)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create database resources: %w", err)
	}

	resources := &DatabaseResources{
		Service: dbSvc,
		Close: func() {
			_ = dbSvc.Close()
		},
	}
	return resources, resources.Close, nil
}

// ProvidePostgresService - 데이터베이스 리소스에서 서비스 추출
func ProvidePostgresService(resources *DatabaseResources) database.Client {
	return resources.Service
}

// ProvideIrisClient - Iris h2c(HTTP/2 Cleartext) 클라이언트 생성
func ProvideIrisClient(cfg config.IrisConfig, logger *slog.Logger) *iris.H2CClient {
	return iris.NewH2CClient(
		cfg.BaseURL,
		cfg.BotToken,
		iris.WithLogger(logger),
		iris.WithTimeout(cfg.HTTPTimeout),
		iris.WithDialTimeout(cfg.HTTPDialTimeout),
		iris.WithResponseHeaderTimeout(cfg.HTTPResponseHeaderTimeout),
	)
}

// ProvideSettingsService - 설정 서비스 생성
func ProvideSettingsService(advanceMinutes []int, scraperProxyEnabled bool, logger *slog.Logger) settings.ReadWriter {
	settingsPath := resolveSettingsFilePath()
	if logger != nil {
		logger.Info("Using settings file path", slog.String("path", settingsPath))
	}

	return settings.NewSettingsService(settingsPath, settings.Settings{
		AlarmAdvanceMinutes: defaultAlarmAdvanceMinute(advanceMinutes),
		ScraperProxyEnabled: scraperProxyEnabled,
	}, logger)
}
