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

func TestObserveSubscriberCacheOnYouTubeStartup_LogsRegistryCount(t *testing.T) {
	cacheSvc := cachemocks.NewStrictClient()
	cacheSvc.SMembersFunc = func(ctx context.Context, key string) ([]string, error) {
		assert.Equal(t, t.Context(), ctx)
		assert.Equal(t, sharedalarmkeys.AlarmChannelRegistryKey, key)
		return []string{"UC_test_1", "UC_test_2"}, nil
	}

	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	err := observeSubscriberCacheOnYouTubeStartup(t.Context(), "youtube-scraper", true, cacheSvc, logger)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), `"msg":"subscriber_cache_observed_on_youtube_startup"`)
	assert.Contains(t, buf.String(), `"runtime":"youtube-scraper"`)
	assert.Contains(t, buf.String(), `"existing_channel_registry_count":2`)
}

func TestObserveSubscriberCacheOnYouTubeStartup_NoOpWhenYouTubeDisabled(t *testing.T) {
	cacheSvc := cachemocks.NewStrictClient()
	called := false
	cacheSvc.SMembersFunc = func(ctx context.Context, key string) ([]string, error) {
		called = true
		return nil, nil
	}

	logger := slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil))

	err := observeSubscriberCacheOnYouTubeStartup(t.Context(), "youtube-scraper", false, cacheSvc, logger)
	require.NoError(t, err)
	assert.False(t, called)
}
