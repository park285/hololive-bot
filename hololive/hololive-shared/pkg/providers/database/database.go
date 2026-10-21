// Package database는 startup PostgreSQL 설정을 DB 리소스로 바꾸고 종료 소유자를 반환한다.
package database

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/park285/shared-go/v2/pkg/db/pgxdb"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/kapu/hololive-shared/pkg/config/settings"
	databasesvc "github.com/kapu/hololive-shared/pkg/service/database"
)

type DatabaseResources struct {
	Service *databasesvc.PostgresService
	Close   func()
}

// RuntimePool은 runtime pool의 통계 라벨과 control 예약 연결 수다. 소유권 갱신·정산 lease를 쓰는 runtime은 1을 두어
// batch 작업이 일반 연결을 모두 잡아도 lease 갱신이 연결을 얻게 한다. 제한 대상은 worker 수가 아니라 실제 DB lease다.
type RuntimePool struct {
	Name          string
	ReservedConns int32
}

// ProvideDatabaseResources는 PostgreSQL pool을 열고 정리 함수를 함께 돌려준다.
// 빈 문자열이 아닌 pool.Name은 pool 통계를 hololive_db_pool_*{pool=pool.Name}으로 기본 registry에 등록하고 정리 때 해제한다.
// 한 프로세스의 pool마다 다른 이름을 써야 하며, 이름이 겹치면 생성이 실패한다.
func ProvideDatabaseResources(
	ctx context.Context,
	postgresConfig *settings.PostgresConfig,
	pool RuntimePool,
	logger *slog.Logger,
) (*DatabaseResources, func(), error) {
	return provideDatabaseResources(ctx, postgresConfig, pool.Name,
		map[string]string{"statement_timeout": "30000", "application_name": runtimeApplicationName(pool.Name)},
		&pgxdb.CancelRequestConfig{CancelRequestDelay: 0, DeadlineDelay: time.Second},
		&pgxdb.AcquirePolicy{Timeout: 5 * time.Second, ReservedConns: pool.ReservedConns}, logger)
}

// ProvideMaintenanceDatabaseResources는 runtime statement/cancel 정책을 상속하지 않는다.
func ProvideMaintenanceDatabaseResources(ctx context.Context, postgresConfig *settings.PostgresConfig, logger *slog.Logger) (*DatabaseResources, func(), error) {
	return provideDatabaseResources(ctx, postgresConfig, "", nil, nil, nil, logger)
}

func provideDatabaseResources(ctx context.Context, postgresConfig *settings.PostgresConfig, poolName string, runtimeParams map[string]string, cancelRequest *pgxdb.CancelRequestConfig, acquirePolicy *pgxdb.AcquirePolicy, logger *slog.Logger) (*DatabaseResources, func(), error) {
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
		RuntimeParams: runtimeParams,
		CancelRequest: cancelRequest,
		AcquirePolicy: acquirePolicy,
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
		Close: sync.OnceFunc(func() {
			unregisterStats()

			if err := dbService.Close(); err != nil && logger != nil {
				logger.Warn("close database resources failed", slog.Any("error", err))
			}
		}),
	}

	return resources, resources.Close, nil
}

func runtimeApplicationName(poolName string) string {
	process := strings.TrimSpace(os.Getenv("PGAPPNAME"))
	if process == "" {
		process = "hololive"
	}

	return process + "/" + poolName
}
