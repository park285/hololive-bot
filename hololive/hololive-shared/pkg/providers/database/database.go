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

	"github.com/park285/shared-go/postgres/pgxdb"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/kapu/hololive-shared/pkg/config/settings"
	databasesvc "github.com/kapu/hololive-shared/pkg/service/database"
)

type DatabaseResources struct {
	Service *databasesvc.PostgresService
	Close   func()
}

type RuntimePool struct {
	Name          string
	ReservedConns int32
}

func ProvideDatabaseResources(
	ctx context.Context,
	postgresConfig *settings.PostgresConfig,
	pool RuntimePool,
	logger *slog.Logger,
) (*DatabaseResources, func(), error) {
	return provideDatabaseResources(ctx, runtimePostgresConfig(postgresConfig, pool), pool.Name, logger)
}

func ProvideMaintenanceDatabaseResources(ctx context.Context, postgresConfig *settings.PostgresConfig, logger *slog.Logger) (*DatabaseResources, func(), error) {
	return provideDatabaseResources(ctx, servicePostgresConfig(postgresConfig), "", logger)
}

func Preflight(postgresConfig *settings.PostgresConfig, pool RuntimePool) error {
	if err := databasesvc.Preflight(runtimePostgresConfig(postgresConfig, pool)); err != nil {
		return fmt.Errorf("preflight database %s: %w", pool.Name, err)
	}

	return nil
}

func runtimePostgresConfig(config *settings.PostgresConfig, pool RuntimePool) *databasesvc.PostgresConfig {
	resolved := servicePostgresConfig(config)
	if resolved == nil {
		return nil
	}

	resolved.RuntimeParams = map[string]string{"statement_timeout": "30000", "application_name": runtimeApplicationName(pool.Name)}
	resolved.CancelRequest = &pgxdb.CancelRequestConfig{CancelRequestDelay: 0, DeadlineDelay: time.Second}
	resolved.AcquirePolicy = &pgxdb.AcquirePolicy{Timeout: 5 * time.Second, ReservedConns: pool.ReservedConns}

	return resolved
}

func servicePostgresConfig(postgresConfig *settings.PostgresConfig) *databasesvc.PostgresConfig {
	if postgresConfig == nil {
		return nil
	}

	return &databasesvc.PostgresConfig{
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
	}
}

func provideDatabaseResources(ctx context.Context, config *databasesvc.PostgresConfig, poolName string, logger *slog.Logger) (*DatabaseResources, func(), error) {
	dbService, err := databasesvc.NewPostgresService(ctx, config, logger)
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
