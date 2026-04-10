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
	MemberRepo  *member.Repository
	MemberCache *member.Cache
	Cleanup     func()
}

func BuildInfraModule(ctx context.Context, cfg *config.Config, logger *slog.Logger) (_ *InfraModule, retErr error) {
	if cfg == nil {
		return nil, fmt.Errorf("build infra module: config is nil")
	}

	cacheResources, cleanupCache, err := providers.ProvideCacheResources(ctx, cfg.Valkey, logger)
	if err != nil {
		return nil, fmt.Errorf("build infra module: provide cache resources: %w", err)
	}
	defer func() {
		if retErr != nil && cleanupCache != nil {
			cleanupCache()
		}
	}()

	databaseResources, cleanupDB, err := providers.ProvideDatabaseResources(ctx, cfg.Postgres, logger)
	if err != nil {
		return nil, fmt.Errorf("build infra module: provide database resources: %w", err)
	}
	defer func() {
		if retErr != nil && cleanupDB != nil {
			cleanupDB()
		}
	}()

	cacheService := cacheResources.Service
	postgresService := databaseResources.Service
	memberRepository := providers.ProvideMemberRepository(postgresService, logger)
	memberCache, err := providers.ProvideMemberCache(ctx, memberRepository, cacheService, logger)
	if err != nil {
		return nil, fmt.Errorf("build infra module: provide member cache: %w", err)
	}

	return &InfraModule{
		Cache:       cacheService,
		Postgres:    postgresService,
		MemberRepo:  memberRepository,
		MemberCache: memberCache,
		Cleanup: func() {
			if cleanupDB != nil {
				cleanupDB()
			}
			if cleanupCache != nil {
				cleanupCache()
			}
		},
	}, nil
}
