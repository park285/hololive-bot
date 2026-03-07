// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package providers

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/database"
	"github.com/kapu/hololive-shared/pkg/service/member"
)

// InfraResources: 캐시/DB/멤버 리소스와 정리 함수를 캡슐화한 구조체.
type InfraResources struct {
	CacheService     cache.Client
	PostgresService  database.Client
	MemberRepository *member.Repository
	MemberCache      *member.Cache
	CleanupCache     func()
	CleanupDB        func()
}

// ProvideInfraResources: 캐시/DB/멤버 리소스를 공통 규약으로 초기화합니다.
func ProvideInfraResources(
	ctx context.Context,
	cfg *config.Config,
	logger *slog.Logger,
) (*InfraResources, error) {
	valkeyConfig := ProvideValkeyConfig(cfg)
	cacheResources, cleanupCache, err := ProvideCacheResources(ctx, valkeyConfig, logger)
	if err != nil {
		return nil, fmt.Errorf("provide cache resources: %w", err)
	}
	cacheService := ProvideCacheService(cacheResources)

	postgresConfig := ProvidePostgresConfig(cfg)
	databaseResources, cleanupDB, err := ProvideDatabaseResources(ctx, postgresConfig, logger)
	if err != nil {
		cleanupCache()
		return nil, fmt.Errorf("provide database resources: %w", err)
	}
	postgresService := ProvidePostgresService(databaseResources)

	memberRepository := ProvideMemberRepository(postgresService, logger)
	memberCache, err := ProvideMemberCache(ctx, memberRepository, cacheService, logger)
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
