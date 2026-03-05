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
