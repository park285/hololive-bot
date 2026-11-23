package testutil

import (
	"context"
	"log/slog"
	"testing"

	"github.com/alicebob/miniredis/v2"

	"github.com/kapu/hololive-shared/internal/testredis"
	"github.com/kapu/hololive-shared/pkg/service/cache"
)

func NewTestCacheService(ctx context.Context, t *testing.T) *cache.Service {
	t.Helper()

	service, _ := NewTestCacheServiceWithMini(ctx, t)

	return service
}

func NewTestCacheServiceWithMini(ctx context.Context, t *testing.T) (*cache.Service, *miniredis.Miniredis) {
	t.Helper()

	host, port, mini := testredis.StartMiniRedis(t)

	service, err := cache.NewCacheService(ctx, cache.Config{
		Host:         host,
		Port:         port,
		DB:           0,
		DisableCache: true,
	}, slog.New(slog.DiscardHandler))
	if err != nil {
		mini.Close()
		t.Fatalf("new cache service: %v", err)
	}

	t.Cleanup(func() {
		if err := service.Close(); err != nil {
			t.Errorf("close cache service: %v", err)
		}

		mini.Close()
	})

	return service, mini
}
