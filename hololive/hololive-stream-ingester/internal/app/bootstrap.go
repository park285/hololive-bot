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
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/database"
	"github.com/kapu/hololive-shared/pkg/service/member"
)

// infraResources 는 캐시/DB 리소스를 담습니다.
type infraResources struct {
	cacheService    cache.Client
	postgresService database.Client
	memberRepo      *member.Repository
	memberCache     *member.Cache
	cleanupCache    func()
	cleanupDB       func()
}

// initInfraResources 는 stream-ingester에 필요한 캐시/DB 리소스를 초기화합니다.
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
