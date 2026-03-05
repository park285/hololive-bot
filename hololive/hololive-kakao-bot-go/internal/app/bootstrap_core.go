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
