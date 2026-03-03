package app

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/service/member"
)

// DBIntegrationRuntime: DB 통합 테스트 및 실행을 위한 런타임 환경 (Repository, Cache, Adapter 포함)
type DBIntegrationRuntime struct {
	Logger        *slog.Logger
	Repository    *member.Repository
	Cache         *member.Cache
	MemberAdapter member.DataProvider

	cleanup func()
}

// Close: 런타임 리소스를 정리하고 연결을 해제합니다.
func (r *DBIntegrationRuntime) Close() {
	if r != nil && r.cleanup != nil {
		r.cleanup()
	}
}

// BuildDBIntegrationRuntime: PostgreSQL 설정을 기반으로 DB 통합 런타임 환경을 구축합니다.
func BuildDBIntegrationRuntime(
	ctx context.Context,
	pgCfg config.PostgresConfig,
	logger *slog.Logger,
) (*DBIntegrationRuntime, error) {
	if logger == nil {
		return nil, fmt.Errorf("logger must not be nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	runtime, cleanup, err := InitializeDBIntegrationRuntime(ctx, pgCfg, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize DB integration runtime: %w", err)
	}
	runtime.cleanup = cleanup

	return runtime, nil
}
