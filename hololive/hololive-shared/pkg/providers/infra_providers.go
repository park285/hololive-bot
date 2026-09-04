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
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/park285/iris-client-go/v2/iris"

	"github.com/kapu/hololive-shared/pkg/config/settings"
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

// ProvideCacheResources - 캐시 리소스 생성 (정리 함수 포함).
func ProvideCacheResources(ctx context.Context, valkeyConfig settings.ValkeyConfig, logger *slog.Logger) (*CacheResources, func(), error) {
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
			if err := cacheClient.Close(); err != nil && logger != nil {
				logger.Warn("close cache resources failed", slog.Any("error", err))
			}
		},
	}

	return resources, resources.Close, nil
}

// ProvideDatabaseResources - 데이터베이스 리소스 생성 (정리 함수 포함).
func ProvideDatabaseResources(ctx context.Context, postgresConfig *settings.PostgresConfig, logger *slog.Logger) (*DatabaseResources, func(), error) {
	if postgresConfig == nil {
		return nil, nil, errors.New("postgres config is nil")
	}

	dbService, err := database.NewPostgresService(ctx, &database.PostgresConfig{
		Host:          postgresConfig.Host,
		Port:          postgresConfig.Port,
		SocketPath:    postgresConfig.SocketPath,
		User:          postgresConfig.User,
		Password:      postgresConfig.Password,
		Database:      postgresConfig.Database,
		SSLMode:       postgresConfig.SSLMode,
		QueryExecMode: postgresConfig.QueryExecMode,
		PoolMinConns:  postgresConfig.PoolMinConns,
		PoolMaxConns:  postgresConfig.PoolMaxConns,
	}, logger)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create database resources: %w", err)
	}

	resources := &DatabaseResources{
		Service: dbService,
		Close: func() {
			if err := dbService.Close(); err != nil && logger != nil {
				logger.Warn("close database resources failed", slog.Any("error", err))
			}
		},
	}

	return resources, resources.Close, nil
}

// ManagedIrisClient는 Hololive runtime이 room 조회와 종료까지 소유하는 Iris 계약입니다.
type ManagedIrisClient interface {
	iris.Client
	GetRooms(ctx context.Context) (*iris.RoomListResponse, error)
	Close() error
}

// ProvideIrisClient - Iris 발송 클라이언트 생성.
func ProvideIrisClient(irisConfig *settings.IrisConfig, logger *slog.Logger, opts ...iris.ClientOption) (ManagedIrisClient, error) {
	out, err := provideRuntimeIrisClient(irisConfig, logger, opts...)
	if err != nil {
		return nil, fmt.Errorf("provide runtime iris client: %w", err)
	}

	return out, nil
}

// IrisKaringClient는 managed Iris 수명주기에 Karing 전송 계약을 더합니다.
type IrisKaringClient interface {
	ManagedIrisClient
	iris.KaringClient
}

func ProvideIrisKaringClient(irisConfig *settings.IrisConfig, logger *slog.Logger, opts ...iris.ClientOption) (IrisKaringClient, error) {
	out, err := provideRuntimeIrisClient(irisConfig, logger, opts...)
	if err != nil {
		return nil, fmt.Errorf("provide runtime iris client: %w", err)
	}

	return out, nil
}

func provideRuntimeIrisClient(irisConfig *settings.IrisConfig, logger *slog.Logger, opts ...iris.ClientOption) (*delivery.RuntimeIrisClient, error) {
	if irisConfig == nil {
		return nil, errors.New("provide iris client: iris config is required")
	}

	resolved := iris.ResolveClientSDKConfig(opts)
	fallbackBaseURL := strings.TrimSpace(resolved.BaseURL)

	if fallbackBaseURL == "" {
		fallbackBaseURL = strings.TrimSpace(irisConfig.BaseURL)
	}

	baseURLFilePath := strings.TrimSpace(irisConfig.BaseURLFile)
	if fallbackBaseURL == "" && baseURLFilePath == "" {
		return nil, errors.New("provide iris client: IRIS_BASE_URL or IRIS_BASE_URL_FILE is required")
	}

	botToken := strings.TrimSpace(resolved.BotToken)
	if botToken == "" {
		botToken = strings.TrimSpace(irisConfig.BotToken)
	}

	if botToken == "" {
		return nil, errors.New("provide iris client: bot token is required")
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
