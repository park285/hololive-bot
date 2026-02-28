package database

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/dbx"
	"gorm.io/gorm"

	"github.com/kapu/hololive-shared/pkg/constants"
)

// PostgresService: PostgreSQL 데이터베이스 연결 및 GORM 인스턴스를 관리하는 서비스
// 내부적으로 shared-go/pkg/dbx.Client를 사용한다.
type PostgresService struct {
	client *dbx.Client
	logger *slog.Logger
}

// PostgresConfig: PostgreSQL 접속 정보(Host, Port, SocketPath, User, Password, Database)를 담는 설정 구조체
type PostgresConfig struct {
	Host             string
	Port             int
	SocketPath       string // UDS 경로 (비어있으면 TCP 사용)
	User             string
	Password         string
	Database         string
	SSLMode          string
	QueryExecMode    string
	PoolMinConns     int
	PoolMaxConns     int
	PoolMaxIdleConns int
}

// NewPostgresService: 주어진 설정을 사용하여 PostgreSQL 연결을 수립하고 서비스를 초기화합니다.
// shared-go/pkg/dbx.Client를 사용하여 pgxpool + GORM 듀얼 구조를 제공한다.
func NewPostgresService(ctx context.Context, cfg PostgresConfig, logger *slog.Logger) (*PostgresService, error) {
	dbxCfg := dbx.Config{
		Host:          cfg.Host,
		Port:          cfg.Port,
		SocketPath:    cfg.SocketPath,
		User:          cfg.User,
		Password:      cfg.Password,
		Name:          cfg.Database,
		SSLMode:       cfg.SSLMode,
		QueryExecMode: cfg.QueryExecMode,
	}
	minConns := cfg.PoolMinConns
	if minConns <= 0 {
		minConns = constants.DatabaseConfig.MaxIdleConns
	}
	maxConns := cfg.PoolMaxConns
	if maxConns <= 0 {
		maxConns = constants.DatabaseConfig.MaxOpenConns
	}
	maxIdleConns := cfg.PoolMaxIdleConns
	if maxIdleConns <= 0 {
		maxIdleConns = constants.DatabaseConfig.MaxIdleConns
	}
	poolCfg := dbx.PoolConfig{
		MaxConns:        maxConns,
		MaxIdleConns:    maxIdleConns,
		MinConns:        minConns,
		ConnMaxLifetime: constants.DatabaseConfig.ConnMaxLifetime,
	}

	retryCfg := dbx.RetryConfig{
		PingTimeout: constants.RequestTimeout.DatabasePing,
	}

	client, err := dbx.Open(ctx, dbxCfg, dbx.OpenOptions{
		Logger: logger,
		Pool:   poolCfg,
		Retry:  retryCfg,
	})
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	return &PostgresService{
		client: client,
		logger: logger,
	}, nil
}

// GetPool: pgxpool.Pool 인스턴스를 반환한다. (raw SQL 사용 시 권장)
func (ps *PostgresService) GetPool() *pgxpool.Pool {
	return ps.client.Pool()
}

// GetDB: database/sql 호환 인터페이스를 반환한다. (하위 호환성용, 권장하지 않음)
//
// Deprecated: Use GetPool() instead for better performance.
func (ps *PostgresService) GetDB() *sql.DB {
	return ps.client.SQL()
}

// GetGormDB: GORM DB 인스턴스를 반환한다. (ORM 기반 DB 조작 시 활용)
func (ps *PostgresService) GetGormDB() *gorm.DB {
	return ps.client.Gorm()
}

// Close: 데이터베이스 연결을 안전하게 종료합니다.
func (ps *PostgresService) Close() error {
	if ps.client != nil {
		if err := ps.client.Close(); err != nil {
			return fmt.Errorf("close database: %w", err)
		}
	}
	return nil
}

// Ping: 데이터베이스 연결 상태를 확인한다. (헬스 체크용)
func (ps *PostgresService) Ping(ctx context.Context) error {
	if err := ps.client.Ping(ctx); err != nil {
		return fmt.Errorf("ping database: %w", err)
	}
	return nil
}
