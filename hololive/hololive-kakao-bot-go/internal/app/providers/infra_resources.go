package providers

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/config"
	sharedproviders "github.com/kapu/hololive-shared/pkg/providers"
)

func ProvideInfraResources(
	ctx context.Context,
	cfg *config.Config,
	logger *slog.Logger,
) (*InfraResources, error) {
	valkeyConfig := sharedproviders.ProvideValkeyConfig(cfg)
	cacheResources, cleanupCache, err := sharedproviders.ProvideCacheResources(ctx, valkeyConfig, logger)
	if err != nil {
		return nil, fmt.Errorf("provide cache resources: %w", err)
	}
	cacheService := sharedproviders.ProvideCacheService(cacheResources)

	postgresConfig := sharedproviders.ProvidePostgresConfig(cfg)
	databaseResources, cleanupDB, err := sharedproviders.ProvideDatabaseResources(ctx, postgresConfig, logger)
	if err != nil {
		cleanupCache()
		return nil, fmt.Errorf("provide database resources: %w", err)
	}
	postgresService := sharedproviders.ProvidePostgresService(databaseResources)

	memberRepository := sharedproviders.ProvideMemberRepository(postgresService, logger)
	memberCache, err := sharedproviders.ProvideMemberCache(ctx, memberRepository, cacheService, logger)
	if err != nil {
		cleanupDB()
		cleanupCache()
		return nil, fmt.Errorf("provide member cache: %w", err)
	}

	return &InfraResources{
		CacheService:     cacheService,
		PostgresService:  postgresService,
		MemberRepository: memberRepository,
		MemberCache:      memberCache,
		CleanupCache:     cleanupCache,
		CleanupDB:        cleanupDB,
	}, nil
}
