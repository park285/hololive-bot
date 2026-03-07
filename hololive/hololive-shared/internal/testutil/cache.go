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

package testutil

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"

	"github.com/kapu/hololive-shared/internal/logging"
	"github.com/kapu/hololive-shared/internal/testredis"
	"github.com/kapu/hololive-shared/pkg/service/cache"
)

// NewTestCacheService는 miniredis 기반 테스트용 캐시 서비스를 생성합니다.
func NewTestCacheService(t *testing.T, ctx context.Context) *cache.Service {
	t.Helper()

	svc, _ := NewTestCacheServiceWithMini(t, ctx)
	return svc
}

// NewTestCacheServiceWithMini는 테스트용 캐시 서비스와 miniredis 인스턴스를 함께 반환합니다.
func NewTestCacheServiceWithMini(t *testing.T, ctx context.Context) (*cache.Service, *miniredis.Miniredis) {
	t.Helper()

	host, port, mini := testredis.StartMiniRedis(t)

	svc, err := cache.NewCacheService(ctx, cache.Config{
		Host:         host,
		Port:         port,
		DB:           0,
		DisableCache: true,
	}, logging.NewTestLogger())
	if err != nil {
		mini.Close()
		t.Fatalf("new cache service: %v", err)
	}

	t.Cleanup(func() {
		_ = svc.Close()
		mini.Close()
	})

	return svc, mini
}
