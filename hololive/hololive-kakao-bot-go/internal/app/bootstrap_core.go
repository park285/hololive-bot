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

// infraResources 는 캐시/DB 리소스를 담는다.
type infraResources struct {
	cacheService    cache.Client
	postgresService database.Client
	memberRepo      *member.Repository
	memberCache     *member.Cache
	cleanupCache    func()
	cleanupDB       func()
}

// initInfraResources 는 캐시/DB 리소스를 초기화한다.
func initInfraResources(ctx context.Context, cfg *config.Config, logger *slog.Logger) (*infraResources, error) {
	resources, err := providers.ProvideInfraResources(ctx, cfg, logger)
	if err != nil {
		return nil, fmt.Errorf("provide infra resources: %w", err)
	}

	return &infraResources{
		cacheService:    resources.CacheService,
		postgresService: resources.PostgresService,
		memberRepo:      resources.MemberRepository,
		memberCache:     resources.MemberCache,
		cleanupCache:    resources.CleanupCache,
		cleanupDB:       resources.CleanupDB,
	}, nil
}

// InitializeWarmMemberCache - cmd/tools/warm_member_cache 전용
func InitializeWarmMemberCache(ctx context.Context, cfg *config.Config, logger *slog.Logger) (*member.Cache, func(), error) {
	postgresConfig := providers.ProvidePostgresConfig(cfg)
	databaseResources, cleanupDB, err := providers.ProvideDatabaseResources(ctx, postgresConfig, logger)
	if err != nil {
		return nil, nil, fmt.Errorf("provide database resources: %w", err)
	}
	postgresService := providers.ProvidePostgresService(databaseResources)
	memberRepository := providers.ProvideMemberRepository(postgresService, logger)

	valkeyConfig := providers.ProvideValkeyConfig(cfg)
	cacheResources, cleanupCache, err := providers.ProvideCacheResources(ctx, valkeyConfig, logger)
	if err != nil {
		cleanupDB()
		return nil, nil, fmt.Errorf("provide cache resources: %w", err)
	}
	cacheService := providers.ProvideCacheService(cacheResources)

	memberCache, err := providers.ProvideMemberCache(ctx, memberRepository, cacheService, logger)
	if err != nil {
		cleanupCache()
		cleanupDB()
		return nil, nil, fmt.Errorf("provide member cache: %w", err)
	}

	cleanup := func() {
		cleanupCache()
		cleanupDB()
	}

	return memberCache, cleanup, nil
}

// InitializeDBIntegrationRuntime - cmd/test_db_integration 전용
func InitializeDBIntegrationRuntime(ctx context.Context, pgCfg config.PostgresConfig, logger *slog.Logger) (*DBIntegrationRuntime, func(), error) {
	databaseResources, cleanupDB, err := providers.ProvideDatabaseResources(ctx, pgCfg, logger)
	if err != nil {
		return nil, nil, fmt.Errorf("provide database resources: %w", err)
	}
	postgresService := providers.ProvidePostgresService(databaseResources)
	memberRepository := providers.ProvideMemberRepository(postgresService, logger)

	memberCache, err := ProvideMemberCacheWithoutValkey(ctx, memberRepository, logger)
	if err != nil {
		cleanupDB()
		return nil, nil, fmt.Errorf("provide member cache without valkey: %w", err)
	}

	memberServiceAdapter := providers.ProvideMemberServiceAdapter(memberCache, logger)

	runtime := &DBIntegrationRuntime{
		Logger:        logger,
		Repository:    memberRepository,
		Cache:         memberCache,
		MemberAdapter: memberServiceAdapter,
	}

	return runtime, cleanupDB, nil
}

// InitializeFetchProfilesRuntime - cmd/tools/fetch_profiles 전용
func InitializeFetchProfilesRuntime(_ context.Context) (*FetchProfilesRuntime, func(), error) {
	logger, cleanupLogger, err := ProvideFetchProfilesLogger()
	if err != nil {
		return nil, nil, fmt.Errorf("provide fetch profiles logger: %w", err)
	}

	httpClient := ProvideFetchProfilesHTTPClient()

	runtime := &FetchProfilesRuntime{
		Logger:     logger,
		HTTPClient: httpClient,
	}

	return runtime, cleanupLogger, nil
}
