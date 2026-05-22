package polltarget

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	sharedalarmkeys "github.com/kapu/hololive-shared/pkg/service/alarm/keys"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
	"github.com/kapu/hololive-shared/pkg/service/database"
	databasemocks "github.com/kapu/hololive-shared/pkg/service/database/mocks"
)

func TestResolveYouTubePollTargets_UsesDBAsAuthoritativeSourceEvenWhenCacheSuperset(t *testing.T) {
	original := loadAlarmChannelIDsFromRepository
	t.Cleanup(func() {
		loadAlarmChannelIDsFromRepository = original
	})

	cache := cachemocks.NewStrictClient()
	cache.SMembersFunc = func(ctx context.Context, key string) ([]string, error) {
		assert.Equal(t, t.Context(), ctx)
		assert.Equal(t, sharedalarmkeys.AlarmChannelRegistryKey, key)
		return []string{"UCmiko", "UCpekora"}, nil
	}

	dbCalls := 0
	loadAlarmChannelIDsFromRepository = func(ctx context.Context, postgresService database.Client) ([]string, error) {
		dbCalls++
		assert.Equal(t, t.Context(), ctx)
		return []string{"UCmiko"}, nil
	}

	targets, err := resolveYouTubePollTargets(
		t.Context(),
		cache,
		&databasemocks.Client{},
		testYouTubePollTargetsOperationalChannels(),
		slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)),
	)
	require.NoError(t, err)

	assert.Equal(t, []string{"UCmiko"}, targets.NotificationChannelIDs)
	assert.Equal(t, []string{"UCpekora", "UCmiko"}, targets.StatsChannelIDs)
	assert.Equal(t, 0, targets.DroppedAlarmTargets)
	assert.Equal(t, 1, dbCalls)
}

func TestResolveYouTubePollTargets_UsesDBAsAuthoritativeSourceOnSameSizeMismatch(t *testing.T) {
	original := loadAlarmChannelIDsFromRepository
	t.Cleanup(func() {
		loadAlarmChannelIDsFromRepository = original
	})

	cache := cachemocks.NewStrictClient()
	cache.SMembersFunc = func(ctx context.Context, key string) ([]string, error) {
		assert.Equal(t, t.Context(), ctx)
		assert.Equal(t, sharedalarmkeys.AlarmChannelRegistryKey, key)
		return []string{"UCpekora"}, nil
	}

	dbCalls := 0
	loadAlarmChannelIDsFromRepository = func(ctx context.Context, postgresService database.Client) ([]string, error) {
		dbCalls++
		assert.Equal(t, t.Context(), ctx)
		return []string{"UCmiko"}, nil
	}

	targets, err := resolveYouTubePollTargets(
		t.Context(),
		cache,
		&databasemocks.Client{},
		testYouTubePollTargetsOperationalChannels(),
		slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)),
	)
	require.NoError(t, err)

	assert.Equal(t, []string{"UCmiko"}, targets.NotificationChannelIDs)
	assert.Equal(t, []string{"UCpekora", "UCmiko"}, targets.StatsChannelIDs)
	assert.Equal(t, 0, targets.DroppedAlarmTargets)
	assert.Equal(t, 1, dbCalls)
}

func TestResolveYouTubePollTargets_LogsStartupSourceDivergence(t *testing.T) {
	original := loadAlarmChannelIDsFromRepository
	t.Cleanup(func() {
		loadAlarmChannelIDsFromRepository = original
	})

	cache := cachemocks.NewStrictClient()
	cache.SMembersFunc = func(ctx context.Context, key string) ([]string, error) {
		assert.Equal(t, t.Context(), ctx)
		assert.Equal(t, sharedalarmkeys.AlarmChannelRegistryKey, key)
		return []string{"UCmiko", "UCpekora"}, nil
	}

	loadAlarmChannelIDsFromRepository = func(ctx context.Context, postgresService database.Client) ([]string, error) {
		assert.Equal(t, t.Context(), ctx)
		return []string{"UCmiko"}, nil
	}

	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	targets, err := resolveYouTubePollTargets(
		t.Context(),
		cache,
		&databasemocks.Client{},
		testYouTubePollTargetsOperationalChannels(),
		logger,
	)
	require.NoError(t, err)

	assert.Equal(t, []string{"UCmiko"}, targets.NotificationChannelIDs)
	assert.Equal(t, []string{"UCpekora", "UCmiko"}, targets.StatsChannelIDs)
	assert.Equal(t, 0, targets.DroppedAlarmTargets)
	assert.Contains(t, buf.String(), `"msg":"youtube_poll_targets_startup_source_diverged"`)
	assert.Contains(t, buf.String(), `"db_notification_target_channels":1`)
	assert.Contains(t, buf.String(), `"cache_notification_target_channels":2`)
	assert.Contains(t, buf.String(), `"cache_only_notification_channels":1`)
	assert.Contains(t, buf.String(), `"db_only_notification_channels":0`)
}

func TestResolveYouTubePollTargets_LogsStartupSourceAlignment(t *testing.T) {
	original := loadAlarmChannelIDsFromRepository
	t.Cleanup(func() {
		loadAlarmChannelIDsFromRepository = original
	})

	cache := cachemocks.NewStrictClient()
	cache.SMembersFunc = func(ctx context.Context, key string) ([]string, error) {
		assert.Equal(t, t.Context(), ctx)
		assert.Equal(t, sharedalarmkeys.AlarmChannelRegistryKey, key)
		return []string{"UCmiko"}, nil
	}

	loadAlarmChannelIDsFromRepository = func(ctx context.Context, postgresService database.Client) ([]string, error) {
		assert.Equal(t, t.Context(), ctx)
		return []string{"UCmiko"}, nil
	}

	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	targets, err := resolveYouTubePollTargets(
		t.Context(),
		cache,
		&databasemocks.Client{},
		testYouTubePollTargetsOperationalChannels(),
		logger,
	)
	require.NoError(t, err)

	assert.Equal(t, []string{"UCmiko"}, targets.NotificationChannelIDs)
	assert.Equal(t, []string{"UCpekora", "UCmiko"}, targets.StatsChannelIDs)
	assert.Equal(t, 0, targets.DroppedAlarmTargets)
	assert.Contains(t, buf.String(), `"msg":"youtube_poll_targets_startup_source_aligned"`)
	assert.Contains(t, buf.String(), `"notification_target_channels":1`)
	assert.Contains(t, buf.String(), `"stats_target_channels":2`)
}

func TestResolveYouTubePollTargets_WarnsWhenCacheInspectionFails(t *testing.T) {
	original := loadAlarmChannelIDsFromRepository
	t.Cleanup(func() {
		loadAlarmChannelIDsFromRepository = original
	})

	cache := cachemocks.NewStrictClient()
	cache.SMembersFunc = func(ctx context.Context, key string) ([]string, error) {
		assert.Equal(t, t.Context(), ctx)
		assert.Equal(t, sharedalarmkeys.AlarmChannelRegistryKey, key)
		return nil, errors.New("cache read failed")
	}

	loadAlarmChannelIDsFromRepository = func(ctx context.Context, postgresService database.Client) ([]string, error) {
		assert.Equal(t, t.Context(), ctx)
		return []string{"UCmiko"}, nil
	}

	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	targets, err := resolveYouTubePollTargets(
		t.Context(),
		cache,
		&databasemocks.Client{},
		testYouTubePollTargetsOperationalChannels(),
		logger,
	)
	require.NoError(t, err)

	assert.Equal(t, []string{"UCmiko"}, targets.NotificationChannelIDs)
	assert.Equal(t, []string{"UCpekora", "UCmiko"}, targets.StatsChannelIDs)
	assert.Equal(t, 0, targets.DroppedAlarmTargets)
	assert.Contains(t, buf.String(), `"msg":"Failed to inspect cache-backed YouTube poll targets at startup"`)
	assert.Contains(t, buf.String(), `"error":"cache read failed"`)
}

func TestResolveYouTubePollTargetsFromAlarmChannelIDs(t *testing.T) {
	t.Parallel()

	targets := resolveYouTubePollTargetsFromAlarmChannelIDs(
		[]string{"UCmiko", "UCunknown", "UCmiko"},
		[]communityShortsOperationalChannel{
			{OwnerLabel: "Pekora", ChannelID: "UCpekora", Enabled: true},
			{OwnerLabel: "Miko", ChannelID: "UCmiko", Enabled: true},
			{OwnerLabel: "Missing", ChannelID: "", Enabled: false},
		},
	)

	assert.Equal(t, []string{"UCmiko"}, targets.NotificationChannelIDs)
	assert.Equal(t, []string{"UCpekora", "UCmiko"}, targets.StatsChannelIDs)
	assert.Equal(t, 1, targets.DroppedAlarmTargets)
}

func testYouTubePollTargetsOperationalChannels() []communityShortsOperationalChannel {
	return []communityShortsOperationalChannel{
		{OwnerLabel: "Pekora", ChannelID: "UCpekora", Enabled: true},
		{OwnerLabel: "Miko", ChannelID: "UCmiko", Enabled: true},
		{OwnerLabel: "Missing", ChannelID: "", Enabled: false},
	}
}
