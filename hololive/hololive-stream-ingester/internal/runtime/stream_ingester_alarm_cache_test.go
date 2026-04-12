package runtime

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	sharedalarm "github.com/kapu/hololive-shared/pkg/service/alarm"
	sharedalarmkeys "github.com/kapu/hololive-shared/pkg/service/alarm/keys"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
	databasemocks "github.com/kapu/hololive-shared/pkg/service/database/mocks"
)

func TestWarmSubscriberCacheFromDBIfCacheCold_RebuildsWhenRegistryEmpty(t *testing.T) {
	original := rebuildSubscriberCacheFromRepository
	t.Cleanup(func() {
		rebuildSubscriberCacheFromRepository = original
	})

	cacheSvc := cachemocks.NewStrictClient()
	cacheSvc.SMembersFunc = func(ctx context.Context, key string) ([]string, error) {
		assert.Equal(t, t.Context(), ctx)
		assert.Equal(t, sharedalarmkeys.AlarmChannelRegistryKey, key)
		return nil, nil
	}

	postgres := &databasemocks.Client{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	called := false
	rebuildSubscriberCacheFromRepository = func(ctx context.Context, cacheService cache.Client, repo *sharedalarm.Repository) (sharedalarm.CacheWarmSummary, error) {
		called = true
		assert.Equal(t, t.Context(), ctx)
		assert.Same(t, cacheSvc, cacheService)
		require.NotNil(t, repo)
		return sharedalarm.CacheWarmSummary{AlarmCount: 1, RoomCount: 1, ChannelCount: 1}, nil
	}

	result, err := warmSubscriberCacheFromDBIfCacheCold(t.Context(), cacheSvc, postgres, logger)
	require.NoError(t, err)
	assert.True(t, result.Rebuilt)
	assert.Equal(t, sharedalarm.CacheWarmSummary{AlarmCount: 1, RoomCount: 1, ChannelCount: 1}, result.Summary)
	assert.True(t, called)
}

func TestWarmSubscriberCacheFromDBIfCacheCold_SkipsWhenRegistryAlreadyPopulated(t *testing.T) {
	original := rebuildSubscriberCacheFromRepository
	t.Cleanup(func() {
		rebuildSubscriberCacheFromRepository = original
	})

	cacheSvc := cachemocks.NewStrictClient()
	cacheSvc.SMembersFunc = func(ctx context.Context, key string) ([]string, error) {
		assert.Equal(t, t.Context(), ctx)
		assert.Equal(t, sharedalarmkeys.AlarmChannelRegistryKey, key)
		return []string{"UC_test"}, nil
	}

	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	called := false
	rebuildSubscriberCacheFromRepository = func(ctx context.Context, cacheService cache.Client, repo *sharedalarm.Repository) (sharedalarm.CacheWarmSummary, error) {
		called = true
		return sharedalarm.CacheWarmSummary{}, nil
	}

	result, err := warmSubscriberCacheFromDBIfCacheCold(t.Context(), cacheSvc, &databasemocks.Client{}, logger)
	require.NoError(t, err)
	assert.False(t, result.Rebuilt)
	assert.Equal(t, sharedalarm.CacheWarmSummary{}, result.Summary)
	assert.False(t, called)
	assert.Contains(t, buf.String(), `"msg":"subscriber_cache_rebuild_skipped"`)
	assert.Contains(t, buf.String(), `"existing_channel_registry_count":1`)
}
