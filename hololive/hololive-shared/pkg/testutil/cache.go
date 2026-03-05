package testutil

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"

	internaltestutil "github.com/kapu/hololive-shared/internal/testutil"
	"github.com/kapu/hololive-shared/pkg/service/cache"
)

// NewTestCacheService는 외부 모듈 테스트에서 사용할 miniredis 기반 캐시 서비스를 생성합니다.
func NewTestCacheService(t *testing.T, ctx context.Context) *cache.Service {
	t.Helper()
	return internaltestutil.NewTestCacheService(t, ctx)
}

// NewTestCacheServiceWithMini는 캐시 서비스와 miniredis 핸들을 함께 반환합니다.
func NewTestCacheServiceWithMini(t *testing.T, ctx context.Context) (*cache.Service, *miniredis.Miniredis) {
	t.Helper()
	return internaltestutil.NewTestCacheServiceWithMini(t, ctx)
}
