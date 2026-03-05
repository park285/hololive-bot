package dbx

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// Client: PostgreSQL 클라이언트 (pgxpool + GORM 듀얼 구조)
type Client struct {
	cfg Config
	opt OpenOptions

	mu     sync.RWMutex
	pool   *pgxpool.Pool
	sqlDB  *sql.DB
	gormDB *gorm.DB
}

// OpenOptions: Open 함수 옵션
type OpenOptions struct {
	Logger     *slog.Logger         // slog 로거 (nil이면 기본 로거 사용)
	Pool       PoolConfig           // 커넥션 풀 설정
	Retry      RetryConfig          // 재시도 설정
	GormLogger gormlogger.Interface // GORM 로거 (nil이면 Silent)

	// DNSFallback: cfg.Host DNS 조회 실패 시 127.0.0.1로 1회 재시도
	// host가 "postgres"인 경우에만 동작 (Docker 환경에서 로컬 실행 시 fallback)
	DNSFallback bool
}

// DefaultOpenOptions: 기본 옵션 반환
func DefaultOpenOptions() OpenOptions {
	return OpenOptions{
		Logger:     slog.Default(),
		Pool:       DefaultPoolConfig(),
		Retry:      DefaultRetryConfig(),
		GormLogger: gormlogger.Default.LogMode(gormlogger.Silent),
	}
}

// Open: PostgreSQL 연결을 수립하고 Client 반환 (즉시 연결)
func Open(ctx context.Context, cfg Config, opt OpenOptions) (*Client, error) {
	client := NewLazy(cfg, opt)
	if err := client.Connect(ctx); err != nil {
		return nil, err
	}
	return client, nil
}

// Pool: pgxpool.Pool 인스턴스 반환 (raw SQL 사용 시 권장)
func (c *Client) Pool() *pgxpool.Pool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.pool
}

// SQL: database/sql 호환 *sql.DB 반환
func (c *Client) SQL() *sql.DB {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.sqlDB
}

// Gorm: GORM DB 인스턴스 반환 (ORM 기반 조작 시 활용)
func (c *Client) Gorm() *gorm.DB {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.gormDB
}

// Ping: 데이터베이스 연결 상태 확인 (헬스 체크용)
func (c *Client) Ping(ctx context.Context) error {
	c.mu.RLock()
	pool := c.pool
	c.mu.RUnlock()

	if pool == nil {
		return fmt.Errorf("pgx pool is nil")
	}
	if err := pool.Ping(ctx); err != nil {
		return fmt.Errorf("postgres ping failed: %w", err)
	}
	return nil
}

// Close: 데이터베이스 연결을 안전하게 종료
// stdlib.OpenDBFromPool로 생성된 *sql.DB는 Close() 시 내부 pgxpool도 함께 닫음
// 따라서 sqlDB.Close()만 호출 (Double Close 방지)
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.sqlDB != nil {
		err := c.sqlDB.Close()
		c.sqlDB = nil
		c.gormDB = nil
		c.pool = nil
		if err != nil {
			return fmt.Errorf("failed to close database connection: %w", err)
		}
		return nil
	}

	if c.pool != nil {
		c.pool.Close()
		c.pool = nil
	}

	return nil
}

// NewLazy: 연결하지 않고 Client만 생성 (lazy-init용)
// 실제 연결은 Connect() 호출 시 수행
func NewLazy(cfg Config, opt OpenOptions) *Client {
	if opt.Logger == nil {
		opt.Logger = slog.Default()
	}
	if opt.GormLogger == nil {
		opt.GormLogger = gormlogger.Default.LogMode(gormlogger.Silent)
	}
	return &Client{
		cfg: cfg,
		opt: opt,
	}
}

// Connect: 최초 1회 실제 연결 수행 (thread-safe)
// 이미 연결된 경우 즉시 반환, 실패 시 다음 호출에서 재시도 가능
func (c *Client) Connect(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.pool != nil {
		return nil
	}

	cfg := c.cfg
	opt := c.opt

	pool := normalizePoolConfig(opt.Pool)
	retry := opt.Retry
	if retry.PingTimeout <= 0 {
		retry.PingTimeout = 5 * time.Second
	}

	client, connectErr := c.tryConnect(ctx, cfg, pool, retry)
	if connectErr != nil && opt.DNSFallback && ShouldFallbackToLocalhost(connectErr, cfg.Host) {
		fallbackCfg := cfg
		fallbackCfg.Host = "127.0.0.1"
		client, connectErr = c.tryConnect(ctx, fallbackCfg, pool, retry)
		if connectErr == nil {
			opt.Logger.Warn("postgres_host_fallback",
				slog.String("configured_host", cfg.Host),
				slog.String("effective_host", "127.0.0.1"),
			)
			c.cfg = fallbackCfg
		}
	}

	if connectErr != nil {
		return connectErr
	}

	connMode := "TCP"
	if c.cfg.SocketPath != "" {
		connMode = "UDS"
	}
	opt.Logger.Info("postgres_pool_connected",
		slog.String("mode", connMode),
		slog.String("host", c.cfg.Host),
		slog.Int("port", c.cfg.Port),
		slog.String("socket_path", c.cfg.SocketPath),
		slog.String("database", c.cfg.Name),
		slog.Int("min_conns", pool.MinConns),
		slog.Int("max_conns", pool.MaxConns),
	)

	c.pool = client.pool
	c.sqlDB = client.sqlDB
	c.gormDB = client.gormDB

	opt.Logger.Info("postgres_gorm_connected")
	return nil
}

// Connected: 연결 상태 확인
func (c *Client) Connected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.pool != nil
}

func (c *Client) tryConnect(ctx context.Context, cfg Config, pool PoolConfig, retry RetryConfig) (*Client, error) {
	// ParseConfig 에러 문자열에 DSN이 포함될 수 있으므로,
	// 파싱 단계에서는 마스킹된 DSN을 사용하고 실제 비밀번호는 이후에 주입한다.
	poolConfig, err := pgxpool.ParseConfig(cfg.SafeDSN())
	if err != nil {
		return nil, fmt.Errorf("parse postgres config: %w", err)
	}
	poolConfig.ConnConfig.Password = cfg.Password
	if cfg.QueryExecMode != "" {
		mode, modeErr := parseQueryExecMode(cfg.QueryExecMode)
		if modeErr != nil {
			return nil, fmt.Errorf("invalid query exec mode: %w", modeErr)
		}
		poolConfig.ConnConfig.DefaultQueryExecMode = mode
	}

	poolConfig.MinConns = int32(pool.MinConns)
	poolConfig.MaxConns = int32(pool.MaxConns)
	poolConfig.MaxConnLifetime = pool.ConnMaxLifetime
	poolConfig.MaxConnIdleTime = pool.ConnMaxIdleTime

	pgxPool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, fmt.Errorf("create pgx pool: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, retry.PingTimeout)
	defer cancel()

	if pingErr := pgxPool.Ping(pingCtx); pingErr != nil {
		pgxPool.Close()
		return nil, fmt.Errorf("postgres ping failed: %w", pingErr)
	}

	sqlDB := stdlib.OpenDBFromPool(pgxPool)
	sqlDB.SetMaxOpenConns(pool.MaxConns)
	sqlDB.SetMaxIdleConns(pool.MaxIdleConns)
	sqlDB.SetConnMaxLifetime(pool.ConnMaxLifetime)
	sqlDB.SetConnMaxIdleTime(pool.ConnMaxIdleTime)

	gormDB, err := gorm.Open(postgres.New(postgres.Config{
		Conn: sqlDB,
	}), &gorm.Config{
		Logger: c.opt.GormLogger,
	})
	if err != nil {
		pgxPool.Close()
		return nil, fmt.Errorf("initialize GORM: %w", err)
	}

	return &Client{
		pool:   pgxPool,
		sqlDB:  sqlDB,
		gormDB: gormDB,
	}, nil
}

func parseQueryExecMode(mode string) (pgx.QueryExecMode, error) {
	switch normalizeQueryExecMode(mode) {
	case "cache_statement":
		return pgx.QueryExecModeCacheStatement, nil
	case "cache_describe":
		return pgx.QueryExecModeCacheDescribe, nil
	case "describe_exec":
		return pgx.QueryExecModeDescribeExec, nil
	case "exec":
		return pgx.QueryExecModeExec, nil
	case "simple_protocol":
		return pgx.QueryExecModeSimpleProtocol, nil
	default:
		return pgx.QueryExecModeCacheStatement, errors.New("allowed: cache_statement, cache_describe, describe_exec, exec, simple_protocol")
	}
}

func normalizePoolConfig(pool PoolConfig) PoolConfig {
	if pool.MinConns <= 0 {
		pool.MinConns = 2
	}
	if pool.MaxConns <= 0 {
		pool.MaxConns = 10
	}
	if pool.MaxIdleConns <= 0 {
		pool.MaxIdleConns = pool.MinConns
	}
	if pool.ConnMaxLifetime <= 0 {
		pool.ConnMaxLifetime = time.Hour
	}
	if pool.ConnMaxIdleTime <= 0 {
		pool.ConnMaxIdleTime = 30 * time.Minute
	}
	return pool
}
