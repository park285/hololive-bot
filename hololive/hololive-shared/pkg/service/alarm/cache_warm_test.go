package alarm

import (
	"context"
	"errors"
	"fmt"
	"sort"
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

func TestRebuildSubscriberCacheFromRepository_RemovesStaleRegistryAndTypedSets(t *testing.T) {
	ctx := t.Context()
	cacheSvc := newMemoryCacheClient()

	_, err := cacheSvc.SAdd(ctx, sharedalarmkeys.AlarmRegistryKey, []string{"room-stale"})
	require.NoError(t, err)
	_, err = cacheSvc.SAdd(ctx, sharedalarmkeys.BuildRoomAlarmKey("room-stale"), []string{"UC_STALE"})
	require.NoError(t, err)
	_, err = cacheSvc.SAdd(ctx, sharedalarmkeys.AlarmChannelRegistryKey, []string{"UC_STALE"})
	require.NoError(t, err)
	_, err = cacheSvc.SAdd(ctx, sharedalarmkeys.BuildChannelSubscriberKey("UC_STALE", domain.AlarmTypeLive), []string{"room-stale"})
	require.NoError(t, err)
	_, err = cacheSvc.SAdd(ctx, sharedalarmkeys.BuildChannelSubscriberKey("UC_ORPHAN", domain.AlarmTypeShorts), []string{"room-orphan"})
	require.NoError(t, err)
	_, err = cacheSvc.SAdd(ctx, sharedalarmkeys.BuildRoomAlarmKey("room-orphan"), []string{"UC_ORPHAN"})
	require.NoError(t, err)
	require.NoError(t, cacheSvc.HSet(ctx, sharedalarmkeys.MemberNameKey, "UC_STALE", "Stale Member"))
	require.NoError(t, cacheSvc.HSet(ctx, sharedalarmkeys.RoomNamesCacheKey, "room-stale", "Stale Room"))
	require.NoError(t, cacheSvc.HSet(ctx, sharedalarmkeys.UserNamesCacheKey, "user-stale", "Stale User"))

	summary, err := rebuildSubscriberCacheFromLoader(ctx, cacheSvc, func(context.Context) ([]*domain.Alarm, error) {
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
	})
	require.NoError(t, err)
	assert.Equal(t, 1, summary.AlarmCount)
	assert.Equal(t, 1, summary.RoomCount)
	assert.Equal(t, 1, summary.ChannelCount)
	assert.Greater(t, summary.KeysDeleted, 0)

	registryRooms, err := cacheSvc.SMembers(ctx, sharedalarmkeys.AlarmRegistryKey)
	require.NoError(t, err)
	assert.Equal(t, []string{"room-fresh"}, registryRooms)

	channelRegistry, err := cacheSvc.SMembers(ctx, sharedalarmkeys.AlarmChannelRegistryKey)
	require.NoError(t, err)
	assert.Equal(t, []string{"UC_FRESH"}, channelRegistry)

	staleRoomMembers, err := cacheSvc.SMembers(ctx, sharedalarmkeys.BuildRoomAlarmKey("room-stale"))
	require.NoError(t, err)
	assert.Empty(t, staleRoomMembers)

	orphanRoomMembers, err := cacheSvc.SMembers(ctx, sharedalarmkeys.BuildRoomAlarmKey("room-orphan"))
	require.NoError(t, err)
	assert.Empty(t, orphanRoomMembers)

	staleLiveSubscribers, err := cacheSvc.SMembers(ctx, sharedalarmkeys.BuildChannelSubscriberKey("UC_STALE", domain.AlarmTypeLive))
	require.NoError(t, err)
	assert.Empty(t, staleLiveSubscribers)

	orphanShortSubscribers, err := cacheSvc.SMembers(ctx, sharedalarmkeys.BuildChannelSubscriberKey("UC_ORPHAN", domain.AlarmTypeShorts))
	require.NoError(t, err)
	assert.Empty(t, orphanShortSubscribers)

	freshCommunitySubscribers, err := cacheSvc.SMembers(ctx, sharedalarmkeys.BuildChannelSubscriberKey("UC_FRESH", domain.AlarmTypeCommunity))
	require.NoError(t, err)
	assert.Equal(t, []string{"room-fresh"}, freshCommunitySubscribers)

	staleMemberName, err := cacheSvc.HGet(ctx, sharedalarmkeys.MemberNameKey, "UC_STALE")
	require.NoError(t, err)
	assert.Empty(t, staleMemberName)

	freshMemberName, err := cacheSvc.HGet(ctx, sharedalarmkeys.MemberNameKey, "UC_FRESH")
	require.NoError(t, err)
	assert.Equal(t, "Fresh Member", freshMemberName)
}

func TestRebuildSubscriberCacheFromRepository_ClearsNegativeCacheKeys(t *testing.T) {
	ctx := t.Context()
	cacheSvc := newMemoryCacheClient()

	require.NoError(t, cacheSvc.Set(ctx, sharedalarmkeys.BuildChannelSubscriberEmptyKey("UC_EMPTY_LIVE", domain.AlarmTypeLive), "1", 0))
	require.NoError(t, cacheSvc.Set(ctx, sharedalarmkeys.BuildChannelSubscriberEmptyKey("UC_EMPTY_SHORTS", domain.AlarmTypeShorts), "1", 0))
	require.NoError(t, cacheSvc.Set(ctx, "other:key", "keep", 0))

	summary, err := rebuildSubscriberCacheFromLoader(ctx, cacheSvc, func(context.Context) ([]*domain.Alarm, error) {
		return nil, nil
	})
	require.NoError(t, err)
	assert.Zero(t, summary.AlarmCount)
	assert.Zero(t, summary.RoomCount)
	assert.Zero(t, summary.ChannelCount)
	assert.Greater(t, summary.KeysDeleted, 0)

	emptyKeys, err := cacheSvc.ScanKeys(ctx, sharedalarmkeys.ChannelSubscribersEmptyKeyPrefix+"*", 500)
	require.NoError(t, err)
	assert.Empty(t, emptyKeys)

	otherExists, err := cacheSvc.Exists(ctx, "other:key")
	require.NoError(t, err)
	assert.True(t, otherExists)
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

func TestRebuildSubscriberCacheFromRepository_LoadError(t *testing.T) {
	ctx := t.Context()
	cacheSvc := newMemoryCacheClient()

	_, err := rebuildSubscriberCacheFromLoader(ctx, cacheSvc, func(context.Context) ([]*domain.Alarm, error) {
		return nil, errors.New("load failed")
	})
	require.Error(t, err)
	assert.ErrorContains(t, err, "rebuild subscriber cache from repository: load alarms")
	assert.ErrorContains(t, err, "load failed")
}

func newMemoryCacheClient() *cachemocks.Client {
	sets := make(map[string]map[string]struct{})
	hashes := make(map[string]map[string]string)
	values := make(map[string]string)

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
	client.SetFunc = func(_ context.Context, key string, value any, _ time.Duration) error {
		values[key] = fmt.Sprint(value)
		return nil
	}
	client.ExistsFunc = func(_ context.Context, key string) (bool, error) {
		if _, ok := sets[key]; ok {
			return true, nil
		}
		if _, ok := hashes[key]; ok {
			return true, nil
		}
		if _, ok := values[key]; ok {
			return true, nil
		}
		return false, nil
	}
	client.DelManyFunc = func(_ context.Context, keys []string) (int64, error) {
		var deleted int64
		for _, key := range keys {
			removed := false
			if _, ok := sets[key]; ok {
				delete(sets, key)
				removed = true
			}
			if _, ok := hashes[key]; ok {
				delete(hashes, key)
				removed = true
			}
			if _, ok := values[key]; ok {
				delete(values, key)
				removed = true
			}
			if removed {
				deleted++
			}
		}
		return deleted, nil
	}
	client.ScanKeysFunc = func(_ context.Context, pattern string, _ int64) ([]string, error) {
		keys := make(map[string]struct{})
		for key := range sets {
			if matchesPattern(key, pattern) {
				keys[key] = struct{}{}
			}
		}
		for key := range hashes {
			if matchesPattern(key, pattern) {
				keys[key] = struct{}{}
			}
		}
		for key := range values {
			if matchesPattern(key, pattern) {
				keys[key] = struct{}{}
			}
		}

		matched := make([]string, 0, len(keys))
		for key := range keys {
			matched = append(matched, key)
		}
		sort.Strings(matched)
		return matched, nil
	}

	return client
}

func matchesPattern(key, pattern string) bool {
	if len(pattern) > 0 && pattern[len(pattern)-1] == '*' {
		return len(key) >= len(pattern)-1 && key[:len(pattern)-1] == pattern[:len(pattern)-1]
	}
	return key == pattern
}
