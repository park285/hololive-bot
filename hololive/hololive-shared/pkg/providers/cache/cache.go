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

// Package cache는 runtime 조립이 Valkey 연결 옵션만으로 캐시 서비스와 정리 함수를 만드는 provider를 담는다.
package cache

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/config/settings"
	cachesvc "github.com/kapu/hololive-shared/pkg/service/cache"
)

type CacheResources struct {
	Service *cachesvc.Service
	Close   func()
}

// ProvideCacheResources는 Valkey 연결 옵션으로 캐시 서비스를 만들고 정리 함수를 함께 돌려준다.
func ProvideCacheResources(ctx context.Context, valkeyConfig settings.ValkeyConfig, logger *slog.Logger) (*CacheResources, func(), error) {
	cacheClient, err := cachesvc.NewCacheService(ctx, cachesvc.Config{
		Host:       valkeyConfig.Host,
		Port:       valkeyConfig.Port,
		Password:   valkeyConfig.Password,
		DB:         valkeyConfig.DB,
		SocketPath: valkeyConfig.SocketPath,
	}, logger)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create cache resources: %w", err)
	}

	resources := &CacheResources{
		Service: cacheClient,
		Close: func() {
			if err := cacheClient.Close(); err != nil && logger != nil {
				logger.Warn("close cache resources failed", slog.Any("error", err))
			}
		},
	}

	return resources, resources.Close, nil
}
