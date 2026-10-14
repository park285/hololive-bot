// Package database는 startup PostgreSQL 설정을 DB 리소스로 바꾸고 종료 소유자를 반환한다.
package database

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/kapu/hololive-shared/pkg/config/settings"
	databasesvc "github.com/kapu/hololive-shared/pkg/service/database"
)

type DatabaseResources struct {
	Service *databasesvc.PostgresService
	Close   func()
}

// ProvideDatabaseResources는 PostgreSQL pool을 열고 정리 함수를 함께 돌려준다.
// 빈 문자열이 아닌 poolName은 pool 통계를 hololive_db_pool_*{pool=poolName}으로 기본 registry에 등록하고 정리 때 해제한다.
// 한 프로세스의 pool마다 다른 이름을 써야 하며, 이름이 겹치면 생성이 실패한다.
func ProvideDatabaseResources(
	ctx context.Context,
	postgresConfig *settings.PostgresConfig,
	poolName string,
	logger *slog.Logger,
) (*DatabaseResources, func(), error) {
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
		return nil, nil, fmt.Errorf("create database resources: %w", err)
	}

	unregisterStats, err := registerPoolStats(prometheus.DefaultRegisterer, dbService.GetPool(), poolName)
	if err != nil {
		return nil, nil, errors.Join(fmt.Errorf("create database resources: %w", err), dbService.Close())
	}

	resources := &DatabaseResources{
		Service: dbService,
		Close: func() {
			unregisterStats()

			if err := dbService.Close(); err != nil && logger != nil {
				logger.Warn("close database resources failed", slog.Any("error", err))
			}
		},
	}

	return resources, resources.Close, nil
}
