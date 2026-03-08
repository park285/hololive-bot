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

package app

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/config"
	providers "github.com/kapu/hololive-shared/pkg/providers"
	"github.com/kapu/hololive-shared/pkg/service/member"
)

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

	memberServiceAdapter := providers.ProvideMemberServiceAdapter(ctx, memberCache, logger)

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
