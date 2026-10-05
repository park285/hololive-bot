package modules

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/kapu/hololive-shared/pkg/config/settings"
	cacheproviders "github.com/kapu/hololive-shared/pkg/providers/cache"
	databaseproviders "github.com/kapu/hololive-shared/pkg/providers/database"
	memberproviders "github.com/kapu/hololive-shared/pkg/providers/member"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/database"
	"github.com/kapu/hololive-shared/pkg/service/member"
)

type InfraModule struct {
	Cache            cache.Client
	Postgres         database.Client
	MemberRepository *member.Repository
	MemberCache      *member.Cache
	StopMemberCache  func()
	Cleanup          func()
}

// InfraOptions는 infra 생성이 실제로 소비하는 cache·DB 설정만 담는다.
type InfraOptions struct {
	Valkey   settings.ValkeyConfig
	Postgres settings.PostgresConfig
}

func BuildInfraModule(ctx context.Context, options InfraOptions, logger *slog.Logger) (_ *InfraModule, retErr error) {
	cacheResources, cleanupCache, err := buildInfraCacheResources(ctx, options.Valkey, logger)
	if err != nil {
		return nil, fmt.Errorf("build infra cache resources: %w", err)
	}

	defer func() {
		cleanupInfraOnError(retErr, cleanupCache)
	}()

	databaseResources, cleanupDB, err := buildInfraDatabaseResources(ctx, &options.Postgres, logger)
	if err != nil {
		return nil, fmt.Errorf("build infra database resources: %w", err)
	}

	defer func() {
		cleanupInfraOnError(retErr, cleanupDB)
	}()

	cacheService := cacheResources.Service
	postgresService := databaseResources.Service
	memberRepository := member.NewMemberRepository(postgresService, logger)

	memberCache, err := buildInfraMemberCache(ctx, memberRepository, cacheService, logger)
	if err != nil {
		return nil, fmt.Errorf("build infra member cache: %w", err)
	}

	return newInfraModule(cacheService, postgresService, memberRepository, memberCache, cleanupDB, cleanupCache), nil
}

func buildInfraCacheResources(
	ctx context.Context,
	valkeyConfig settings.ValkeyConfig,
	logger *slog.Logger,
) (*cacheproviders.CacheResources, func(), error) {
	cacheResources, cleanupCache, err := cacheproviders.ProvideCacheResources(ctx, valkeyConfig, logger)
	if err != nil {
		return nil, nil, fmt.Errorf("build infra module: provide cache resources: %w", err)
	}

	return cacheResources, cleanupCache, nil
}

func buildInfraDatabaseResources(
	ctx context.Context,
	postgresConfig *settings.PostgresConfig,
	logger *slog.Logger,
) (*databaseproviders.DatabaseResources, func(), error) {
	databaseResources, cleanupDB, err := databaseproviders.ProvideDatabaseResources(ctx, postgresConfig, logger)
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
	memberCache, err := memberproviders.ProvideMemberCache(ctx, memberRepository, cacheService, logger)
	if err != nil {
		return nil, fmt.Errorf("build infra module: provide member cache: %w", err)
	}

	return memberCache, nil
}

func cleanupInfraOnError(retErr error, cleanup func()) {
	if retErr != nil && cleanup != nil {
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
	stopMemberCache := sync.OnceFunc(memberCache.Close)

	return &InfraModule{
		Cache:            cacheService,
		Postgres:         postgresService,
		MemberRepository: memberRepository,
		MemberCache:      memberCache,
		StopMemberCache:  stopMemberCache,
		Cleanup: sync.OnceFunc(func() {
			stopMemberCache()

			if cleanupDB != nil {
				cleanupDB()
			}

			if cleanupCache != nil {
				cleanupCache()
			}
		}),
	}
}
