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
func ProvideCacheResources(ctx context.Context, valkeyConfig config.ValkeyConfig, logger *slog.Logger) (*CacheResources, func(), error) {
	cacheClient, err := cache.NewCacheService(ctx, cache.Config{
		Host:       valkeyConfig.Host,
		Port:       valkeyConfig.Port,
		Password:   valkeyConfig.Password,
		DB:         valkeyConfig.DB,
		SocketPath: valkeyConfig.SocketPath,
	}, logger)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create cache resources: %w", err)
	}

	resources := &CacheResources{
		Service: cacheClient,
		Close: func() {
			_ = cacheClient.Close()
		},
	}
	return resources, resources.Close, nil
}

// ProvideDatabaseResources - 데이터베이스 리소스 생성 (정리 함수 포함)
func ProvideDatabaseResources(ctx context.Context, postgresConfig config.PostgresConfig, logger *slog.Logger) (*DatabaseResources, func(), error) {
	dbService, err := database.NewPostgresService(ctx, database.PostgresConfig{
		Host:             postgresConfig.Host,
		Port:             postgresConfig.Port,
		SocketPath:       postgresConfig.SocketPath,
		User:             postgresConfig.User,
		Password:         postgresConfig.Password,
		Database:         postgresConfig.Database,
		SSLMode:          postgresConfig.SSLMode,
		QueryExecMode:    postgresConfig.QueryExecMode,
		PoolMinConns:     postgresConfig.PoolMinConns,
		PoolMaxConns:     postgresConfig.PoolMaxConns,
		PoolMaxIdleConns: postgresConfig.PoolMaxIdleConns,
	}, logger)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create database resources: %w", err)
	}

	resources := &DatabaseResources{
		Service: dbService,
		Close: func() {
			_ = dbService.Close()
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
	irisConfig := iris.ResolveClientSDKConfig(opts)
	fallbackBaseURL := strings.TrimSpace(irisConfig.BaseURL)
	if fallbackBaseURL == "" {
		fallbackBaseURL = strings.TrimSpace(os.Getenv(iris.EnvBaseURL))
	}
	baseURLFilePath := strings.TrimSpace(os.Getenv(irisBaseURLFileEnv))
	if fallbackBaseURL == "" && baseURLFilePath == "" {
		return nil, fmt.Errorf("provide iris client: IRIS_BASE_URL or IRIS_BASE_URL_FILE is required")
	}

	botToken := strings.TrimSpace(irisConfig.BotToken)
	if botToken == "" {
		botToken = strings.TrimSpace(os.Getenv(iris.EnvBotToken))
	}
	if botToken == "" {
		return nil, fmt.Errorf("provide iris client: bot token is required")
	}

	client := delivery.NewRuntimeIrisClient(
		fallbackBaseURL,
		botToken,
		baseURLFilePath,
		logger,
		opts...,
	)
	if err := client.ValidateBaseURL(); err != nil {
		return nil, fmt.Errorf("provide iris client: %w", err)
	}
	return client, nil
}
