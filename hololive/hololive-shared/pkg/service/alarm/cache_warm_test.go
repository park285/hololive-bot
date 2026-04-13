package alarm

import (
	"context"
	"errors"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	sharedalarmkeys "github.com/kapu/hololive-shared/pkg/service/alarm/keys"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWarmSubscriberCacheFromAlarms_WritesTypeSpecificSubscriptions(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	cacheSvc := newMemoryCacheClient()

	summary, err := WarmSubscriberCacheFromAlarms(ctx, cacheSvc, []*domain.Alarm{
		{
			RoomID:     "room-community",
			UserID:     "user-community",
			ChannelID:  "UC_A",
			MemberName: "Member A",
			RoomName:   "Community Room",
			UserName:   "Community User",
			AlarmTypes: domain.AlarmTypes{domain.AlarmTypeCommunity},
		},
		{
			RoomID:     "room-shorts",
			UserID:     "user-shorts",
			ChannelID:  "UC_A",
			MemberName: "Member A",
			RoomName:   "Shorts Room",
			UserName:   "Shorts User",
			AlarmTypes: domain.AlarmTypes{domain.AlarmTypeShorts},
		},
		{
			RoomID:     "room-default",
			UserID:     "user-default",
			ChannelID:  "UC_B",
			MemberName: "Member B",
			RoomName:   "Default Room",
			UserName:   "Default User",
		},
	})
	require.NoError(t, err)
	assert.Equal(t, CacheWarmSummary{AlarmCount: 3, RoomCount: 3, ChannelCount: 2}, summary)

	roomChannels, err := cacheSvc.SMembers(ctx, sharedalarmkeys.BuildRoomAlarmKey("room-community"))
	require.NoError(t, err)
	assert.Equal(t, []string{"UC_A"}, roomChannels)

	registryRooms, err := cacheSvc.SMembers(ctx, sharedalarmkeys.AlarmRegistryKey)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"room-community", "room-shorts", "room-default"}, registryRooms)

	channelRegistry, err := cacheSvc.SMembers(ctx, sharedalarmkeys.AlarmChannelRegistryKey)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"UC_A", "UC_B"}, channelRegistry)

	communitySubs, err := cacheSvc.SMembers(ctx, sharedalarmkeys.BuildChannelSubscriberKey("UC_A", domain.AlarmTypeCommunity))
	require.NoError(t, err)
	assert.Equal(t, []string{"room-community"}, communitySubs)

	shortsSubs, err := cacheSvc.SMembers(ctx, sharedalarmkeys.BuildChannelSubscriberKey("UC_A", domain.AlarmTypeShorts))
	require.NoError(t, err)
	assert.Equal(t, []string{"room-shorts"}, shortsSubs)

	liveSubs, err := cacheSvc.SMembers(ctx, sharedalarmkeys.BuildChannelSubscriberKey("UC_A", domain.AlarmTypeLive))
	require.NoError(t, err)
	assert.Empty(t, liveSubs)

	defaultLiveSubs, err := cacheSvc.SMembers(ctx, sharedalarmkeys.BuildChannelSubscriberKey("UC_B", domain.AlarmTypeLive))
	require.NoError(t, err)
	assert.Equal(t, []string{"room-default"}, defaultLiveSubs)

	defaultCommunitySubs, err := cacheSvc.SMembers(ctx, sharedalarmkeys.BuildChannelSubscriberKey("UC_B", domain.AlarmTypeCommunity))
	require.NoError(t, err)
	assert.Equal(t, []string{"room-default"}, defaultCommunitySubs)

	defaultShortsSubs, err := cacheSvc.SMembers(ctx, sharedalarmkeys.BuildChannelSubscriberKey("UC_B", domain.AlarmTypeShorts))
	require.NoError(t, err)
	assert.Equal(t, []string{"room-default"}, defaultShortsSubs)

	memberName, err := cacheSvc.HGet(ctx, sharedalarmkeys.MemberNameKey, "UC_A")
	require.NoError(t, err)
	assert.Equal(t, "Member A", memberName)

	roomName, err := cacheSvc.HGet(ctx, sharedalarmkeys.RoomNamesCacheKey, "room-shorts")
	require.NoError(t, err)
	assert.Equal(t, "Shorts Room", roomName)

	userName, err := cacheSvc.HGet(ctx, sharedalarmkeys.UserNamesCacheKey, "user-default")
	require.NoError(t, err)
	assert.Equal(t, "Default User", userName)
}

func TestWarmSubscriberCacheFromRepository_RemainsAdditiveByContract(t *testing.T) {
	ctx := t.Context()
	cacheSvc := newMemoryCacheClient()
	originalLoader := loadAllAlarmsFromRepository
	loadAllAlarmsFromRepository = func(context.Context, *Repository) ([]*domain.Alarm, error) {
		return []*domain.Alarm{
			{
				RoomID:     "room-fresh",
				UserID:     "user-fresh",
				ChannelID:  "UC_FRESH",
				MemberName: "Fresh Member",
				RoomName:   "Fresh Room",
				UserName:   "Fresh User",
				AlarmTypes: domain.AlarmTypes{domain.AlarmTypeCommunity},
			},
		}, nil
	}
	t.Cleanup(func() {
		loadAllAlarmsFromRepository = originalLoader
	})

	_, err := cacheSvc.SAdd(ctx, sharedalarmkeys.AlarmRegistryKey, []string{"room-existing"})
	require.NoError(t, err)
	_, err = cacheSvc.SAdd(ctx, sharedalarmkeys.BuildRoomAlarmKey("room-existing"), []string{"UC_EXISTING"})
	require.NoError(t, err)
	_, err = cacheSvc.SAdd(ctx, sharedalarmkeys.AlarmChannelRegistryKey, []string{"UC_EXISTING"})
	require.NoError(t, err)
	_, err = cacheSvc.SAdd(ctx, sharedalarmkeys.BuildChannelSubscriberKey("UC_EXISTING", domain.AlarmTypeLive), []string{"room-existing"})
	require.NoError(t, err)

	summary, err := WarmSubscriberCacheFromRepository(ctx, cacheSvc, &Repository{})
	require.NoError(t, err)
	assert.Equal(t, CacheWarmSummary{AlarmCount: 1, RoomCount: 1, ChannelCount: 1}, summary)

	registryRooms, err := cacheSvc.SMembers(ctx, sharedalarmkeys.AlarmRegistryKey)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"room-existing", "room-fresh"}, registryRooms)

	channelRegistry, err := cacheSvc.SMembers(ctx, sharedalarmkeys.AlarmChannelRegistryKey)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"UC_EXISTING", "UC_FRESH"}, channelRegistry)

	existingLiveSubscribers, err := cacheSvc.SMembers(ctx, sharedalarmkeys.BuildChannelSubscriberKey("UC_EXISTING", domain.AlarmTypeLive))
	require.NoError(t, err)
	assert.Equal(t, []string{"room-existing"}, existingLiveSubscribers)

	freshCommunitySubscribers, err := cacheSvc.SMembers(ctx, sharedalarmkeys.BuildChannelSubscriberKey("UC_FRESH", domain.AlarmTypeCommunity))
	require.NoError(t, err)
	assert.Equal(t, []string{"room-fresh"}, freshCommunitySubscribers)
}

func TestWarmSubscriberCacheFromRepository_LoadError(t *testing.T) {
	ctx := t.Context()
	originalLoader := loadAllAlarmsFromRepository
	loadAllAlarmsFromRepository = func(context.Context, *Repository) ([]*domain.Alarm, error) {
		return nil, errors.New("load failed")
	}
	t.Cleanup(func() {
		loadAllAlarmsFromRepository = originalLoader
	})

	_, err := WarmSubscriberCacheFromRepository(ctx, newMemoryCacheClient(), &Repository{})
	require.Error(t, err)
	assert.ErrorContains(t, err, "warm subscriber cache from repository: load alarms")
	assert.ErrorContains(t, err, "load failed")
}

func TestRebuildSubscriberCacheFromRepository_ReplacesStaleCacheState(t *testing.T) {
	ctx := t.Context()
	cacheSvc := newMemoryCacheClient()
	originalLoader := loadAllAlarmsFromRepository
	loadAllAlarmsFromRepository = func(context.Context, *Repository) ([]*domain.Alarm, error) {
		return []*domain.Alarm{
			{
				RoomID:     "room-fresh",
				UserID:     "user-fresh",
				ChannelID:  "UC_FRESH",
				MemberName: "Fresh Member",
				RoomName:   "Fresh Room",
				UserName:   "Fresh User",
				AlarmTypes: domain.AlarmTypes{domain.AlarmTypeCommunity},
			},
		}, nil
	}
	t.Cleanup(func() {
		loadAllAlarmsFromRepository = originalLoader
	})

	_, err := cacheSvc.SAdd(ctx, sharedalarmkeys.AlarmRegistryKey, []string{"room-stale"})
	require.NoError(t, err)
	_, err = cacheSvc.SAdd(ctx, sharedalarmkeys.BuildRoomAlarmKey("room-stale"), []string{"UC_STALE"})
	require.NoError(t, err)
	_, err = cacheSvc.SAdd(ctx, sharedalarmkeys.AlarmChannelRegistryKey, []string{"UC_STALE"})
	require.NoError(t, err)
	_, err = cacheSvc.SAdd(ctx, sharedalarmkeys.BuildChannelSubscriberKey("UC_STALE", domain.AlarmTypeLive), []string{"room-stale"})
	require.NoError(t, err)
	require.NoError(t, cacheSvc.HSet(ctx, sharedalarmkeys.MemberNameKey, "UC_STALE", "Stale Member"))
	require.NoError(t, cacheSvc.HSet(ctx, sharedalarmkeys.RoomNamesCacheKey, "room-stale", "Stale Room"))
	require.NoError(t, cacheSvc.HSet(ctx, sharedalarmkeys.UserNamesCacheKey, "user-stale", "Stale User"))
	require.NoError(t, cacheSvc.Set(ctx, sharedalarmkeys.BuildChannelSubscriberEmptyKey("UC_STALE", domain.AlarmTypeLive), "1", time.Minute))

	summary, err := RebuildSubscriberCacheFromRepository(ctx, cacheSvc, &Repository{})
	require.NoError(t, err)
	assert.Equal(t, CacheWarmSummary{AlarmCount: 1, RoomCount: 1, ChannelCount: 1}, summary)

	registryRooms, err := cacheSvc.SMembers(ctx, sharedalarmkeys.AlarmRegistryKey)
	require.NoError(t, err)
	assert.Equal(t, []string{"room-fresh"}, registryRooms)

	channelRegistry, err := cacheSvc.SMembers(ctx, sharedalarmkeys.AlarmChannelRegistryKey)
	require.NoError(t, err)
	assert.Equal(t, []string{"UC_FRESH"}, channelRegistry)

	staleRoomChannels, err := cacheSvc.SMembers(ctx, sharedalarmkeys.BuildRoomAlarmKey("room-stale"))
	require.NoError(t, err)
	assert.Empty(t, staleRoomChannels)

	staleLiveSubscribers, err := cacheSvc.SMembers(ctx, sharedalarmkeys.BuildChannelSubscriberKey("UC_STALE", domain.AlarmTypeLive))
	require.NoError(t, err)
	assert.Empty(t, staleLiveSubscribers)

	staleEmptyKnown, err := cacheSvc.Exists(ctx, sharedalarmkeys.BuildChannelSubscriberEmptyKey("UC_STALE", domain.AlarmTypeLive))
	require.NoError(t, err)
	assert.False(t, staleEmptyKnown)

	staleMemberName, err := cacheSvc.HGet(ctx, sharedalarmkeys.MemberNameKey, "UC_STALE")
	require.NoError(t, err)
	assert.Empty(t, staleMemberName)

	freshCommunitySubscribers, err := cacheSvc.SMembers(ctx, sharedalarmkeys.BuildChannelSubscriberKey("UC_FRESH", domain.AlarmTypeCommunity))
	require.NoError(t, err)
	assert.Equal(t, []string{"room-fresh"}, freshCommunitySubscribers)
}

func newMemoryCacheClient() *cachemocks.Client {
	sets := make(map[string]map[string]struct{})
	hashes := make(map[string]map[string]string)
	values := make(map[string]struct{})

	client := cachemocks.NewStrictClient()
	client.SAddFunc = func(_ context.Context, key string, members []string) (int64, error) {
		if sets[key] == nil {
			sets[key] = make(map[string]struct{})
		}
		var added int64
		for _, member := range members {
			if _, exists := sets[key][member]; exists {
				continue
			}
			sets[key][member] = struct{}{}
			added++
		}
		return added, nil
	}
	client.SMembersFunc = func(_ context.Context, key string) ([]string, error) {
		members := make([]string, 0, len(sets[key]))
		for member := range sets[key] {
			members = append(members, member)
		}
		sort.Strings(members)
		return members, nil
	}
	client.HSetFunc = func(_ context.Context, key, field, value string) error {
		if hashes[key] == nil {
			hashes[key] = make(map[string]string)
		}
		hashes[key][field] = value
		return nil
	}
	client.HGetFunc = func(_ context.Context, key, field string) (string, error) {
		if hashes[key] == nil {
			return "", nil
		}
		return hashes[key][field], nil
	}
	client.SetFunc = func(_ context.Context, key string, _ any, _ time.Duration) error {
		values[key] = struct{}{}
		return nil
	}
	client.ExistsFunc = func(_ context.Context, key string) (bool, error) {
		_, exists := values[key]
		return exists, nil
	}
	client.ScanKeysFunc = func(_ context.Context, pattern string, _ int64) ([]string, error) {
		prefix := strings.TrimSuffix(pattern, "*")
		keys := make([]string, 0)
		seen := make(map[string]struct{})
		for key := range sets {
			if strings.HasPrefix(key, prefix) {
				seen[key] = struct{}{}
			}
		}
		for key := range hashes {
			if strings.HasPrefix(key, prefix) {
				seen[key] = struct{}{}
			}
		}
		for key := range values {
			if strings.HasPrefix(key, prefix) {
				seen[key] = struct{}{}
			}
		}
		for key := range seen {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		return keys, nil
	}
	client.DelManyFunc = func(_ context.Context, keys []string) (int64, error) {
		var deleted int64
		for _, key := range keys {
			if _, exists := sets[key]; exists {
				delete(sets, key)
				deleted++
			}
			if _, exists := hashes[key]; exists {
				delete(hashes, key)
				deleted++
			}
			if _, exists := values[key]; exists {
				delete(values, key)
				deleted++
			}
		}
		return deleted, nil
	}

	return client
}
