package bootstrap

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/adapter"
	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/database"
	"github.com/kapu/hololive-shared/pkg/service/template"
	"github.com/kapu/hololive-shared/pkg/util"
)

// CacheResources: 초기화된 캐시 서비스 인스턴스와 리소스 해제(Close) 함수를 캡슐화한 구조체
type CacheResources struct {
	Service *cache.Service
	Close   func()
}

// DatabaseResources: 초기화된 DB 서비스 인스턴스와 리소스 해제(Close) 함수를 캡슐화한 구조체
type DatabaseResources struct {
	Service *database.PostgresService
	Close   func()
}

// NewLogger: 설정(Config)을 기반으로 새로운 slog 로거 인스턴스를 생성합니다.
func NewLogger(cfg *config.Config) (*slog.Logger, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config must not be nil")
	}
	logger, err := util.EnableFileLoggingWithLevel(util.LogConfig{
		Dir:        cfg.Logging.Dir,
		MaxSizeMB:  cfg.Logging.MaxSizeMB,
		MaxBackups: cfg.Logging.MaxBackups,
		MaxAgeDays: cfg.Logging.MaxAgeDays,
		Compress:   cfg.Logging.Compress,
	}, "bot.log", cfg.Logging.Level)
	if err != nil {
		return nil, fmt.Errorf("create logger: %w", err)
	}
	return logger, nil
}

// NewMessageStack: 메시지 파싱(Adapter) 및 포맷팅(Formatter) 유틸리티 인스턴스를 생성하여 반환합니다.
func NewMessageStack(prefix string, renderer *template.Renderer) (*adapter.MessageAdapter, *adapter.ResponseFormatter) {
	return adapter.NewMessageAdapter(prefix), adapter.NewResponseFormatter(prefix, renderer)
}

// NewCacheResources: Redis(Valkey) 설정을 기반으로 캐시 서비스를 초기화하고 리소스 객체를 반환합니다.
// SocketPath가 설정되면 UDS로 연결합니다.
func NewCacheResources(ctx context.Context, cfg config.ValkeyConfig, logger *slog.Logger) (*CacheResources, error) {
	cacheSvc, err := cache.NewCacheService(ctx, cache.Config{
		Host:       cfg.Host,
		Port:       cfg.Port,
		Password:   cfg.Password,
		DB:         cfg.DB,
		SocketPath: cfg.SocketPath,
	}, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create cache service: %w", err)
	}

	res := &CacheResources{
		Service: cacheSvc,
		Close: func() {
			_ = cacheSvc.Close()
		},
	}
	return res, nil
}

// NewDatabaseResources: PostgreSQL 설정을 기반으로 DB 서비스를 초기화하고 리소스 객체를 반환합니다.
// SocketPath가 설정되면 UDS로 연결합니다.
func NewDatabaseResources(ctx context.Context, cfg config.PostgresConfig, logger *slog.Logger) (*DatabaseResources, error) {
	dbSvc, err := database.NewPostgresService(ctx, database.PostgresConfig{
		Host:             cfg.Host,
		Port:             cfg.Port,
		SocketPath:       cfg.SocketPath,
		User:             cfg.User,
		Password:         cfg.Password,
		Database:         cfg.Database,
		SSLMode:          cfg.SSLMode,
		QueryExecMode:    cfg.QueryExecMode,
		PoolMinConns:     cfg.PoolMinConns,
		PoolMaxConns:     cfg.PoolMaxConns,
		PoolMaxIdleConns: cfg.PoolMaxIdleConns,
	}, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create postgres service: %w", err)
	}

	res := &DatabaseResources{
		Service: dbSvc,
		Close: func() {
			_ = dbSvc.Close()
		},
	}
	return res, nil
}
