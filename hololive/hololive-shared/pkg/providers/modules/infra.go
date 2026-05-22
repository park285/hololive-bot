package modules

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/providers"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/database"
	"github.com/kapu/hololive-shared/pkg/service/member"
)

type InfraModule struct {
	Cache       cache.Client
	Postgres    database.Client
	MemberRepository  *member.Repository
	MemberCache *member.Cache
	Cleanup     func()
}

func BuildInfraModule(ctx context.Context, cfg *config.Config, logger *slog.Logger) (_ *InfraModule, retErr error) {
	if cfg == nil {
		return nil, fmt.Errorf("build infra module: config is nil")
	}

	cacheResources, cleanupCache, err := buildInfraCacheResources(ctx, cfg, logger)
	if err != nil {
		return nil, err
	}
	defer cleanupInfraOnError(&retErr, cleanupCache)

	databaseResources, cleanupDB, err := buildInfraDatabaseResources(ctx, cfg, logger)
	if err != nil {
		return nil, err
	}
	defer cleanupInfraOnError(&retErr, cleanupDB)

	cacheService := cacheResources.Service
	postgresService := databaseResources.Service
	memberRepository := providers.ProvideMemberRepository(postgresService, logger)
	memberCache, err := buildInfraMemberCache(ctx, memberRepository, cacheService, logger)
	if err != nil {
		return nil, err
	}

	return newInfraModule(cacheService, postgresService, memberRepository, memberCache, cleanupDB, cleanupCache), nil
}

func buildInfraCacheResources(
	ctx context.Context,
	cfg *config.Config,
	logger *slog.Logger,
) (*providers.CacheResources, func(), error) {
	cacheResources, cleanupCache, err := providers.ProvideCacheResources(ctx, cfg.Valkey, logger)
	if err != nil {
		return nil, nil, fmt.Errorf("build infra module: provide cache resources: %w", err)
	}
	return cacheResources, cleanupCache, nil
}

func buildInfraDatabaseResources(
	ctx context.Context,
	cfg *config.Config,
	logger *slog.Logger,
) (*providers.DatabaseResources, func(), error) {
	databaseResources, cleanupDB, err := providers.ProvideDatabaseResources(ctx, cfg.Postgres, logger)
	if err != nil {
		return nil, nil, fmt.Errorf("build infra module: provide database resources: %w", err)
	}
	return databaseResources, cleanupDB, nil
}

func buildInfraMemberCache(
	ctx context.Context,
	memberRepository *member.Repository,
	cacheService cache.Client,
	logger *slog.Logger,
) (*member.Cache, error) {
	memberCache, err := providers.ProvideMemberCache(ctx, memberRepository, cacheService, logger)
	if err != nil {
		return nil, fmt.Errorf("build infra module: provide member cache: %w", err)
	}
	return memberCache, nil
}

func cleanupInfraOnError(retErr *error, cleanup func()) {
	if retErr != nil && *retErr != nil && cleanup != nil {
		cleanup()
	}
}

func newInfraModule(
	cacheService cache.Client,
	postgresService database.Client,
	memberRepository *member.Repository,
	memberCache *member.Cache,
	cleanupDB func(),
	cleanupCache func(),
) *InfraModule {
	return &InfraModule{
		Cache:       cacheService,
		Postgres:    postgresService,
		MemberRepository:  memberRepository,
		MemberCache: memberCache,
		Cleanup: func() {
			if cleanupDB != nil {
				cleanupDB()
			}
			if cleanupCache != nil {
				cleanupCache()
			}
		},
	}
}
