// Package database는 startup PostgreSQL 설정을 DB 리소스로 바꾸고 종료 소유자를 반환한다.
package database

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/config/settings"
	databasesvc "github.com/kapu/hololive-shared/pkg/service/database"
)

type DatabaseResources struct {
	Service *databasesvc.PostgresService
	Close   func()
}

// ProvideDatabaseResources - 데이터베이스 리소스 생성 (정리 함수 포함).
func ProvideDatabaseResources(ctx context.Context, postgresConfig *settings.PostgresConfig, logger *slog.Logger) (*DatabaseResources, func(), error) {
	if postgresConfig == nil {
		return nil, nil, errors.New("postgres config is nil")
	}

	dbService, err := databasesvc.NewPostgresService(ctx, &databasesvc.PostgresConfig{
		Host:          postgresConfig.Host,
		Port:          postgresConfig.Port,
		SocketPath:    postgresConfig.SocketPath,
		User:          postgresConfig.User,
		Password:      postgresConfig.Password,
		Database:      postgresConfig.Database,
		SSLMode:       postgresConfig.SSLMode,
		SSLRootCert:   postgresConfig.SSLRootCert,
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
