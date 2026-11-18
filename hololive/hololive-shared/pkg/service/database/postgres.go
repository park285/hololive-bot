package database

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/park285/shared-go/postgres/pgxdb"

	"github.com/kapu/hololive-shared/pkg/constants"
)

type PostgresService struct {
	pool   *pgxpool.Pool
	logger *slog.Logger
}

type PostgresConfig struct {
	Host          string
	Port          int
	SocketPath    string
	User          string
	Password      string
	Database      string
	SSLMode       string
	SSLRootCert   string
	QueryExecMode string
	PoolMinConns  int
	PoolMaxConns  int
	RuntimeParams map[string]string
	CancelRequest *pgxdb.CancelRequestConfig
	AcquirePolicy *pgxdb.AcquirePolicy
}

func NewPostgresService(ctx context.Context, config *PostgresConfig, logger *slog.Logger) (*PostgresService, error) {
	cfg, opts, err := postgresOptions(config)
	if err != nil {
		return nil, err
	}

	opts.Logger = logger

	pool, err := pgxdb.OpenPool(ctx, cfg, opts)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	return &PostgresService{pool: pool, logger: logger}, nil
}

func Preflight(config *PostgresConfig) error {
	cfg, opts, err := postgresOptions(config)
	if err != nil {
		return err
	}

	if preflightErr := pgxdb.Preflight(cfg, opts); preflightErr != nil {
		return fmt.Errorf("preflight database config: %w", preflightErr)
	}

	return nil
}

func postgresOptions(config *PostgresConfig) (pgxdb.Config, pgxdb.Options, error) {
	if config == nil {
		return pgxdb.Config{}, pgxdb.Options{}, errors.New("postgres config is nil")
	}

	sslMode := strings.TrimSpace(config.SSLMode)
	if sslMode == "" {
		sslMode = "verify-full"
	}

	minConns, maxConns, err := resolvePoolConns(config)
	if err != nil {
		return pgxdb.Config{}, pgxdb.Options{}, fmt.Errorf("resolve pool conns: %w", err)
	}

	return pgxdb.Config{
		Host:          config.Host,
		Port:          config.Port,
		SocketPath:    config.SocketPath,
		User:          config.User,
		Password:      config.Password,
		Name:          config.Database,
		SSLMode:       sslMode,
		SSLRootCert:   config.SSLRootCert,
		QueryExecMode: config.QueryExecMode,
	}, pgxdb.Options{
		RuntimeParams: config.RuntimeParams,
		CancelRequest: config.CancelRequest,
		AcquirePolicy: config.AcquirePolicy,
		Pool: pgxdb.PoolConfig{
			MinConns:              minConns,
			MaxConns:              maxConns,
			ConnMaxLifetime:       constants.DatabaseConfig.ConnMaxLifetime,
			ConnMaxLifetimeJitter: constants.DatabaseConfig.ConnMaxLifetimeJitter,
			ConnMaxIdleTime:       constants.DatabaseConfig.ConnMaxIdleTime,
			AcquirePingTimeout:    time.Second,
		},
		Ping: pgxdb.PingConfig{
			PingTimeout: constants.RequestTimeout.DatabasePing,
		},
	}, nil
}

func resolvePoolConns(config *PostgresConfig) (minConns, maxConns int, err error) {
	if config.PoolMinConns < 0 {
		return 0, 0, fmt.Errorf("postgres pool min conns must not be negative: %d", config.PoolMinConns)
	}

	maxConns = config.PoolMaxConns
	if maxConns <= 0 {
		return 0, 0, fmt.Errorf("postgres pool max conns must be positive: %d", maxConns)
	}

	if config.PoolMinConns > maxConns {
		return 0, 0, fmt.Errorf("postgres pool min conns %d exceeds max conns %d", config.PoolMinConns, maxConns)
	}

	return config.PoolMinConns, maxConns, nil
}

func (ps *PostgresService) GetPool() *pgxpool.Pool {
	return ps.pool
}

func (ps *PostgresService) Close() error {
	if ps.pool != nil {
		ps.pool.Close()
	}

	return nil
}

func (ps *PostgresService) Ping(ctx context.Context) error {
	if ps.pool == nil {
		return errors.New("ping database: pool is nil")
	}

	if err := ps.pool.Ping(ctx); err != nil {
		return fmt.Errorf("ping database: %w", err)
	}

	return nil
}
