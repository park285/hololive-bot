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
	"os"
	"strings"

	"github.com/park285/iris-client-go/iris"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/database"
	"github.com/kapu/hololive-shared/pkg/service/delivery"
)

type CacheResources struct {
	Service *cache.Service
	Close   func()
}

type DatabaseResources struct {
	Service *database.PostgresService
	Close   func()
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

const irisBaseURLFileEnv = "IRIS_BASE_URL_FILE"

// ProvideIrisClient - Iris 발송 클라이언트 생성
func ProvideIrisClient(logger *slog.Logger, opts ...iris.ClientOption) (iris.Client, error) {
	return provideRuntimeIrisClient(logger, opts...)
}

type IrisKaringClient interface {
	iris.Client
	iris.KaringClient
}

func ProvideIrisKaringClient(logger *slog.Logger, opts ...iris.ClientOption) (IrisKaringClient, error) {
	return provideRuntimeIrisClient(logger, opts...)
}

func provideRuntimeIrisClient(logger *slog.Logger, opts ...iris.ClientOption) (*delivery.RuntimeIrisClient, error) {
	cfg := iris.ResolveClientSDKConfig(opts)
	fallbackBaseURL := strings.TrimSpace(cfg.BaseURL)
	if fallbackBaseURL == "" {
		fallbackBaseURL = strings.TrimSpace(os.Getenv(iris.EnvBaseURL))
	}
	baseURLFilePath := strings.TrimSpace(os.Getenv(irisBaseURLFileEnv))
	if fallbackBaseURL == "" && baseURLFilePath == "" {
		return nil, fmt.Errorf("provide iris client: IRIS_BASE_URL or IRIS_BASE_URL_FILE is required")
	}

	botToken := strings.TrimSpace(cfg.BotToken)
	if botToken == "" {
		botToken = strings.TrimSpace(os.Getenv(iris.EnvBotToken))
	}
	if botToken == "" {
		return nil, fmt.Errorf("provide iris client: bot token is required")
	}

	return delivery.NewRuntimeIrisClient(
		fallbackBaseURL,
		botToken,
		baseURLFilePath,
		logger,
		opts...,
	), nil
}
