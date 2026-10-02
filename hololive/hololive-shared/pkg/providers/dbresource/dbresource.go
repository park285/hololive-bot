// Package dbresource는 runtime이 PostgreSQL 연결 pool과 정리 함수를 만드는 작은 조립 패키지다.
// providers의 다른 조립(Holodex, scraper, member, Iris)을 함께 링크하지 않도록 DB 생성만 둔다.
package dbresource

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/config/settings"
	"github.com/kapu/hololive-shared/pkg/service/database"
)

type Resources struct {
	Service *database.PostgresService
	Close   func()
}

// ProvideDatabaseResources - 데이터베이스 리소스 생성 (정리 함수 포함).
func Provide(ctx context.Context, postgresConfig *settings.PostgresConfig, logger *slog.Logger) (*Resources, func(), error) {
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
		SSLRootCert:   postgresConfig.SSLRootCert,
		QueryExecMode: postgresConfig.QueryExecMode,
		PoolMinConns:  postgresConfig.PoolMinConns,
		PoolMaxConns:  postgresConfig.PoolMaxConns,
	}, logger)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create database resources: %w", err)
	}

	resources := &Resources{
		Service: dbService,
		Close: func() {
			if err := dbService.Close(); err != nil && logger != nil {
				logger.Warn("close database resources failed", slog.Any("error", err))
			}
		},
	}

	return resources, resources.Close, nil
}
