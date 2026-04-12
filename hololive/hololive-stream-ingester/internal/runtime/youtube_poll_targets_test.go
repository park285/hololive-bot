package runtime

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	sharedalarmkeys "github.com/kapu/hololive-shared/pkg/service/alarm/keys"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
	"github.com/kapu/hololive-shared/pkg/service/database"
	databasemocks "github.com/kapu/hololive-shared/pkg/service/database/mocks"
)

func TestResolveYouTubePollTargets_UsesCacheBeforeDB(t *testing.T) {
	original := loadAlarmChannelIDsFromRepository
	t.Cleanup(func() {
		loadAlarmChannelIDsFromRepository = original
	})

	cacheSvc := cachemocks.NewStrictClient()
	cacheSvc.SMembersFunc = func(ctx context.Context, key string) ([]string, error) {
		assert.Equal(t, t.Context(), ctx)
		assert.Equal(t, sharedalarmkeys.AlarmChannelRegistryKey, key)
		return []string{"UCmiko", "UCunknown", "UCmiko"}, nil
	}

	dbCalls := 0
	loadAlarmChannelIDsFromRepository = func(ctx context.Context, postgresService database.Client) ([]string, error) {
		dbCalls++
		assert.Equal(t, t.Context(), ctx)
		return []string{"UCmiko"}, nil
	}

	targets, err := resolveYouTubePollTargets(
		t.Context(),
		cacheSvc,
		&databasemocks.Client{},
		[]communityShortsOperationalChannel{
			{ownerLabel: "Pekora", channelID: "UCpekora", enabled: true},
			{ownerLabel: "Miko", channelID: "UCmiko", enabled: true},
			{ownerLabel: "Missing", channelID: "", enabled: false},
		},
	)
	require.NoError(t, err)

	assert.Equal(t, []string{"UCmiko"}, targets.NotificationChannelIDs)
	assert.Equal(t, []string{"UCpekora", "UCmiko"}, targets.StatsChannelIDs)
	assert.Equal(t, 1, targets.DroppedAlarmTargets)
	assert.Equal(t, 1, dbCalls)
}

func TestResolveYouTubePollTargets_FallsBackToDBWhenCacheEmpty(t *testing.T) {
	original := loadAlarmChannelIDsFromRepository
	t.Cleanup(func() {
		loadAlarmChannelIDsFromRepository = original
	})

	cacheSvc := cachemocks.NewStrictClient()
	cacheSvc.SMembersFunc = func(ctx context.Context, key string) ([]string, error) {
		assert.Equal(t, t.Context(), ctx)
		assert.Equal(t, sharedalarmkeys.AlarmChannelRegistryKey, key)
		return nil, nil
	}

	dbCalls := 0
	loadAlarmChannelIDsFromRepository = func(ctx context.Context, postgresService database.Client) ([]string, error) {
		dbCalls++
		assert.Equal(t, t.Context(), ctx)
		return []string{"UCmiko"}, nil
	}

	targets, err := resolveYouTubePollTargets(
		t.Context(),
		cacheSvc,
		&databasemocks.Client{},
		[]communityShortsOperationalChannel{
			{ownerLabel: "Pekora", channelID: "UCpekora", enabled: true},
			{ownerLabel: "Miko", channelID: "UCmiko", enabled: true},
		},
	)
	require.NoError(t, err)

	assert.Equal(t, []string{"UCmiko"}, targets.NotificationChannelIDs)
	assert.Equal(t, []string{"UCpekora", "UCmiko"}, targets.StatsChannelIDs)
	assert.Equal(t, 0, targets.DroppedAlarmTargets)
	assert.Equal(t, 1, dbCalls)
}

func TestResolveYouTubePollTargets_UsesDBWhenCacheCandidateShrinks(t *testing.T) {
	original := loadAlarmChannelIDsFromRepository
	t.Cleanup(func() {
		loadAlarmChannelIDsFromRepository = original
	})

	cacheSvc := cachemocks.NewStrictClient()
	cacheSvc.SMembersFunc = func(ctx context.Context, key string) ([]string, error) {
		assert.Equal(t, t.Context(), ctx)
		assert.Equal(t, sharedalarmkeys.AlarmChannelRegistryKey, key)
		return []string{"UCmiko"}, nil
	}

	dbCalls := 0
	loadAlarmChannelIDsFromRepository = func(ctx context.Context, postgresService database.Client) ([]string, error) {
		dbCalls++
		assert.Equal(t, t.Context(), ctx)
		return []string{"UCmiko", "UCpekora"}, nil
	}

	targets, err := resolveYouTubePollTargets(
		t.Context(),
		cacheSvc,
		&databasemocks.Client{},
		[]communityShortsOperationalChannel{
			{ownerLabel: "Pekora", channelID: "UCpekora", enabled: true},
			{ownerLabel: "Miko", channelID: "UCmiko", enabled: true},
		},
	)
	require.NoError(t, err)

	assert.Equal(t, []string{"UCmiko", "UCpekora"}, targets.NotificationChannelIDs)
	assert.Equal(t, []string{"UCpekora", "UCmiko"}, targets.StatsChannelIDs)
	assert.Equal(t, 0, targets.DroppedAlarmTargets)
	assert.Equal(t, 1, dbCalls)
}

func TestResolveYouTubePollTargetsFromAlarmChannelIDs(t *testing.T) {
	t.Parallel()

	targets := resolveYouTubePollTargetsFromAlarmChannelIDs(
		[]string{"UCmiko", "UCunknown", "UCmiko"},
		[]communityShortsOperationalChannel{
			{ownerLabel: "Pekora", channelID: "UCpekora", enabled: true},
			{ownerLabel: "Miko", channelID: "UCmiko", enabled: true},
			{ownerLabel: "Missing", channelID: "", enabled: false},
		},
	)

	assert.Equal(t, []string{"UCmiko"}, targets.NotificationChannelIDs)
	assert.Equal(t, []string{"UCpekora", "UCmiko"}, targets.StatsChannelIDs)
	assert.Equal(t, 1, targets.DroppedAlarmTargets)
}
