package modules

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/kapu/hololive-shared/pkg/config/settings"
	databaseproviders "github.com/kapu/hololive-shared/pkg/providers/database"
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
// Pool.Name은 DB pool 통계 지표의 pool 라벨이며, 한 프로세스 안에서 pool마다 달라야 한다.
type InfraOptions struct {
	Valkey   settings.ValkeyConfig
	Postgres settings.PostgresConfig
	Pool     databaseproviders.RuntimePool
}

func BuildInfraModule(ctx context.Context, options InfraOptions, logger *slog.Logger) (_ *InfraModule, retErr error) {
	cacheService, err := cache.NewCacheService(ctx, cache.Config{
		Host:       options.Valkey.Host,
		Port:       options.Valkey.Port,
		Password:   options.Valkey.Password,
		DB:         options.Valkey.DB,
		SocketPath: options.Valkey.SocketPath,
	}, logger)
	if err != nil {
		return nil, fmt.Errorf("build infra module: create cache service: %w", err)
	}

	cleanupCache := func() {
		if closeErr := cacheService.Close(); closeErr != nil && logger != nil {
			logger.Warn("close cache resources failed", slog.Any("error", closeErr))
		}
	}

	defer func() {
		cleanupInfraOnError(retErr, cleanupCache)
	}()

	databaseResources, cleanupDB, err := databaseproviders.ProvideDatabaseResources(ctx, &options.Postgres, options.Pool, logger)
	if err != nil {
		return nil, fmt.Errorf("build infra module: provide database resources: %w", err)
	}

	defer func() {
		cleanupInfraOnError(retErr, cleanupDB)
	}()

	postgresService := databaseResources.Service
	memberRepository := member.NewMemberRepository(postgresService, logger)

	memberCache, err := member.NewMemberCache(ctx, memberRepository, cacheService, logger, member.CacheConfig{WarmUp: true})
	if err != nil {
		return nil, fmt.Errorf("build infra module: create member cache: %w", err)
	}

	return newInfraModule(cacheService, postgresService, memberRepository, memberCache, cleanupDB, cleanupCache), nil
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
