package app

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/config"
	providers "github.com/kapu/hololive-shared/pkg/providers"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/database"
	"github.com/kapu/hololive-shared/pkg/service/member"
)

// infraResources 는 캐시/DB 리소스를 담습니다.
type infraResources struct {
	cacheService    *cache.Service
	postgresService *database.PostgresService
	memberRepo      *member.Repository
	memberCache     *member.Cache
	cleanupCache    func()
	cleanupDB       func()
}

// initInfraResources 는 stream-ingester에 필요한 캐시/DB 리소스를 초기화합니다.
func initInfraResources(ctx context.Context, cfg *config.Config, logger *slog.Logger) (*infraResources, error) {
	valkeyConfig := providers.ProvideValkeyConfig(cfg)
	cacheResources, cleanupCache, err := providers.ProvideCacheResources(ctx, valkeyConfig, logger)
	if err != nil {
		return nil, fmt.Errorf("provide cache resources: %w", err)
	}
	cacheService := providers.ProvideCacheService(cacheResources)

	postgresConfig := providers.ProvidePostgresConfig(cfg)
	databaseResources, cleanupDB, err := providers.ProvideDatabaseResources(ctx, postgresConfig, logger)
	if err != nil {
		cleanupCache()
		return nil, fmt.Errorf("provide database resources: %w", err)
	}
	postgresService := providers.ProvidePostgresService(databaseResources)

	memberRepository := providers.ProvideMemberRepository(postgresService, logger)
	memberCache, err := providers.ProvideMemberCache(ctx, memberRepository, cacheService, logger)
	if err != nil {
		cleanupDB()
		cleanupCache()
		return nil, fmt.Errorf("provide member cache: %w", err)
	}

	return &infraResources{
		cacheService:    cacheService,
		postgresService: postgresService,
		memberRepo:      memberRepository,
		memberCache:     memberCache,
		cleanupCache:    cleanupCache,
		cleanupDB:       cleanupDB,
	}, nil
}
