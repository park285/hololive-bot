package alarmcache

import (
	"bytes"
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	sharedalarmkeys "github.com/kapu/hololive-shared/pkg/service/alarm/keys"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
)

func TestObserveSubscriberCacheOnProducerStartup_LogsRegistryCount(t *testing.T) {
	cache := cachemocks.NewStrictClient()
	cache.SMembersFunc = func(ctx context.Context, key string) ([]string, error) {
		assert.Equal(t, t.Context(), ctx)
		assert.Equal(t, sharedalarmkeys.AlarmChannelRegistryKey, key)
		return []string{"UC_test_1", "UC_test_2"}, nil
	}

	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	err := observeSubscriberCacheOnProducerStartup(t.Context(), "youtube-producer", true, cache, logger)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), `"msg":"subscriber_cache_observed_on_producer_startup"`)
	assert.Contains(t, buf.String(), `"runtime":"youtube-producer"`)
	assert.Contains(t, buf.String(), `"existing_channel_registry_count":2`)
}

func TestObserveSubscriberCacheOnProducerStartup_NoOpWhenYouTubeDisabled(t *testing.T) {
	cache := cachemocks.NewStrictClient()
	called := false
	cache.SMembersFunc = func(ctx context.Context, key string) ([]string, error) {
		called = true
		return nil, nil
	}

	logger := slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil))

	err := observeSubscriberCacheOnProducerStartup(t.Context(), "youtube-producer", false, cache, logger)
	require.NoError(t, err)
	assert.False(t, called)
}
