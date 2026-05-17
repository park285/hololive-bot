package alarm

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	contractsalarm "github.com/kapu/hololive-shared/pkg/contracts/alarm"
	"github.com/kapu/hololive-shared/pkg/domain"
	sharedalarmkeys "github.com/kapu/hololive-shared/pkg/service/alarm/keys"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
	"github.com/kapu/hololive-shared/pkg/util"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/valkey-io/valkey-go"
)

func TestWarmSubscriberCacheFromAlarms_WritesTypeSpecificSubscriptions(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	cacheSvc := newMemoryCacheClient(t)

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

func TestWarmSubscriberCacheFromAlarms_MarksEmptyCacheState(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	cacheSvc := newMemoryCacheClient(t)

	summary, err := WarmSubscriberCacheFromAlarms(ctx, cacheSvc, nil)
	require.NoError(t, err)
	assert.Equal(t, CacheWarmSummary{}, summary)

	emptyMarkerExists, err := cacheSvc.Exists(ctx, sharedalarmkeys.AlarmSubscriberCacheEmptyKey)
	require.NoError(t, err)
	assert.True(t, emptyMarkerExists)

	channelRegistryExists, err := cacheSvc.Exists(ctx, sharedalarmkeys.AlarmChannelRegistryKey)
	require.NoError(t, err)
	assert.False(t, channelRegistryExists)

	versionExists, err := cacheSvc.Exists(ctx, sharedalarmkeys.AlarmChannelRegistryVersionKey)
	require.NoError(t, err)
	assert.True(t, versionExists)
}

func TestWarmSubscriberCacheFromAlarms_ClearsEmptyCacheMarkerWhenAlarmsExist(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	cacheSvc := newMemoryCacheClient(t)
	require.NoError(t, cacheSvc.Set(ctx, sharedalarmkeys.AlarmSubscriberCacheEmptyKey, "1", 0))

	_, err := WarmSubscriberCacheFromAlarms(ctx, cacheSvc, []*domain.Alarm{
		{
			RoomID:    "room-1",
			UserID:    "user-1",
			ChannelID: "UC_ONE",
		},
	})
	require.NoError(t, err)

	emptyMarkerExists, err := cacheSvc.Exists(ctx, sharedalarmkeys.AlarmSubscriberCacheEmptyKey)
	require.NoError(t, err)
	assert.False(t, emptyMarkerExists)

	versionExists, err := cacheSvc.Exists(ctx, sharedalarmkeys.AlarmChannelRegistryVersionKey)
	require.NoError(t, err)
	assert.True(t, versionExists)
}

func TestWarmSubscriberCacheFromAlarms_UsesBatchedWrites(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	baseCache := newMemoryCacheClient(t)
	countingCache := &countingWarmCacheClient{Client: baseCache}

	alarms := make([]*domain.Alarm, 0, 48)
	for i := range 48 {
		roomID := "room-" + strconv.Itoa(i)
		userID := "user-" + strconv.Itoa(i)

		alarms = append(alarms, &domain.Alarm{
			RoomID:     roomID,
			UserID:     userID,
			ChannelID:  "UC_BATCH",
			MemberName: "Member " + strconv.Itoa(i),
			RoomName:   "Room " + strconv.Itoa(i),
			UserName:   "User " + strconv.Itoa(i),
		})
	}

	summary, err := WarmSubscriberCacheFromAlarms(ctx, countingCache, alarms)
	require.NoError(t, err)
	assert.Equal(t, CacheWarmSummary{AlarmCount: 48, RoomCount: 48, ChannelCount: 1}, summary)
	assert.Less(t, countingCache.sAddCalls, len(alarms)*(3+len(domain.DefaultAlarmTypes)))
	assert.Zero(t, countingCache.hSetCalls)
	assert.Equal(t, 3, countingCache.hmSetCalls)

	liveSubscribers, err := countingCache.SMembers(ctx, sharedalarmkeys.BuildChannelSubscriberKey("UC_BATCH", domain.AlarmTypeLive))
	require.NoError(t, err)
	assert.Len(t, liveSubscribers, len(alarms))
}

func TestWarmSubscriberCacheFromRepository_RemainsAdditiveByContract(t *testing.T) {
	ctx := t.Context()
	cacheSvc := newMemoryCacheClient(t)
	originalLoader := loadAllAlarmsFromRepository
	originalMemberNameLoader := loadMemberNamesFromRepository
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
	loadMemberNamesFromRepository = func(context.Context, *Repository) (map[string]string, error) {
		return map[string]string{"UC_FRESH": "라덴"}, nil
	}
	t.Cleanup(func() {
		loadAllAlarmsFromRepository = originalLoader
		loadMemberNamesFromRepository = originalMemberNameLoader
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

	memberName, err := cacheSvc.HGet(ctx, sharedalarmkeys.MemberNameKey, "UC_FRESH")
	require.NoError(t, err)
	assert.Equal(t, "라덴", memberName)
}

func TestWarmSubscriberCacheFromRepository_UsesAuthoritativeMemberNames(t *testing.T) {
	ctx := t.Context()
	cacheSvc := newMemoryCacheClient(t)
	originalLoader := loadAllAlarmsFromRepository
	originalMemberNameLoader := loadMemberNamesFromRepository
	loadAllAlarmsFromRepository = func(context.Context, *Repository) ([]*domain.Alarm, error) {
		return []*domain.Alarm{
			{
				RoomID:     "room-1",
				UserID:     "user-1",
				ChannelID:  "UC_RADEN",
				MemberName: "Juufuutei Raden",
				RoomName:   "room",
				UserName:   "user",
				AlarmTypes: domain.AlarmTypes{domain.AlarmTypeLive},
			},
		}, nil
	}
	loadMemberNamesFromRepository = func(context.Context, *Repository) (map[string]string, error) {
		return map[string]string{"UC_RADEN": "라덴"}, nil
	}
	t.Cleanup(func() {
		loadAllAlarmsFromRepository = originalLoader
		loadMemberNamesFromRepository = originalMemberNameLoader
	})

	_, err := WarmSubscriberCacheFromRepository(ctx, cacheSvc, &Repository{})
	require.NoError(t, err)

	memberName, err := cacheSvc.HGet(ctx, sharedalarmkeys.MemberNameKey, "UC_RADEN")
	require.NoError(t, err)
	assert.Equal(t, "라덴", memberName)
}

func TestWarmSubscriberCacheFromRepository_LoadError(t *testing.T) {
	ctx := t.Context()
	originalLoader := loadAllAlarmsFromRepository
	originalMemberNameLoader := loadMemberNamesFromRepository
	loadAllAlarmsFromRepository = func(context.Context, *Repository) ([]*domain.Alarm, error) {
		return nil, errors.New("load failed")
	}
	loadMemberNamesFromRepository = func(context.Context, *Repository) (map[string]string, error) {
		return nil, nil
	}
	t.Cleanup(func() {
		loadAllAlarmsFromRepository = originalLoader
		loadMemberNamesFromRepository = originalMemberNameLoader
	})

	_, err := WarmSubscriberCacheFromRepository(ctx, newMemoryCacheClient(t), &Repository{})
	require.Error(t, err)
	assert.ErrorContains(t, err, "warm subscriber cache from repository: load alarms")
	assert.ErrorContains(t, err, "load failed")
}

func TestRebuildSubscriberCacheFromRepository_MemberNameLoadErrorPreservesExistingCache(t *testing.T) {
	ctx := t.Context()
	cacheSvc := newMemoryCacheClient(t)
	originalLoader := loadAllAlarmsFromRepository
	originalMemberNameLoader := loadMemberNamesFromRepository
	loadAllAlarmsFromRepository = func(context.Context, *Repository) ([]*domain.Alarm, error) {
		return []*domain.Alarm{
			{
				RoomID:     "room-fresh",
				UserID:     "user-fresh",
				ChannelID:  "UC_FRESH",
				MemberName: "Fresh Member",
				RoomName:   "Fresh Room",
				UserName:   "Fresh User",
				AlarmTypes: domain.AlarmTypes{domain.AlarmTypeLive},
			},
		}, nil
	}
	loadMemberNamesFromRepository = func(context.Context, *Repository) (map[string]string, error) {
		return nil, errors.New("member names unavailable")
	}
	t.Cleanup(func() {
		loadAllAlarmsFromRepository = originalLoader
		loadMemberNamesFromRepository = originalMemberNameLoader
	})

	_, err := cacheSvc.SAdd(ctx, sharedalarmkeys.AlarmRegistryKey, []string{"room-existing"})
	require.NoError(t, err)
	_, err = cacheSvc.SAdd(ctx, sharedalarmkeys.BuildRoomAlarmKey("room-existing"), []string{"UC_EXISTING"})
	require.NoError(t, err)
	_, err = cacheSvc.SAdd(ctx, sharedalarmkeys.AlarmChannelRegistryKey, []string{"UC_EXISTING"})
	require.NoError(t, err)
	_, err = cacheSvc.SAdd(ctx, sharedalarmkeys.BuildChannelSubscriberKey("UC_EXISTING", domain.AlarmTypeLive), []string{"room-existing"})
	require.NoError(t, err)
	require.NoError(t, cacheSvc.HSet(ctx, sharedalarmkeys.MemberNameKey, "UC_EXISTING", "Existing Member"))

	_, err = RebuildSubscriberCacheFromRepository(ctx, cacheSvc, &Repository{})
	require.Error(t, err)
	assert.ErrorContains(t, err, "rebuild subscriber cache from repository: load member names")

	registryRooms, err := cacheSvc.SMembers(ctx, sharedalarmkeys.AlarmRegistryKey)
	require.NoError(t, err)
	assert.Equal(t, []string{"room-existing"}, registryRooms)

	existingRoomChannels, err := cacheSvc.SMembers(ctx, sharedalarmkeys.BuildRoomAlarmKey("room-existing"))
	require.NoError(t, err)
	assert.Equal(t, []string{"UC_EXISTING"}, existingRoomChannels)

	existingMemberName, err := cacheSvc.HGet(ctx, sharedalarmkeys.MemberNameKey, "UC_EXISTING")
	require.NoError(t, err)
	assert.Equal(t, "Existing Member", existingMemberName)
}

func TestCompactUniqueStrings_TrimsDedupesAndPreservesOrder(t *testing.T) {
	t.Parallel()

	values := []string{" room-1 ", "", "room-2", "room-1", " room-3 ", "room-2", "room-4"}

	assert.Equal(t, []string{"room-1", "room-2", "room-3", "room-4"}, compactUniqueStrings(values))
}

func TestRebuildSubscriberCacheFromRepository_ReplacesStaleCacheState(t *testing.T) {
	ctx := t.Context()
	cacheSvc := newMemoryCacheClient(t)
	originalLoader := loadAllAlarmsFromRepository
	originalMemberNameLoader := loadMemberNamesFromRepository
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
	loadMemberNamesFromRepository = func(context.Context, *Repository) (map[string]string, error) {
		return map[string]string{"UC_FRESH": "Fresh Member"}, nil
	}
	t.Cleanup(func() {
		loadAllAlarmsFromRepository = originalLoader
		loadMemberNamesFromRepository = originalMemberNameLoader
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

func TestRebuildSubscriberCacheFromRepository_RemovesOrphanRoomKeysAndPreservesDispatchQueue(t *testing.T) {
	ctx := t.Context()
	cacheSvc := newMemoryCacheClient(t)
	originalLoader := loadAllAlarmsFromRepository
	originalMemberNameLoader := loadMemberNamesFromRepository
	loadAllAlarmsFromRepository = func(context.Context, *Repository) ([]*domain.Alarm, error) {
		return []*domain.Alarm{
			{
				RoomID:     "room-fresh",
				UserID:     "user-fresh",
				ChannelID:  "UC_FRESH",
				MemberName: "Fresh Member",
				RoomName:   "Fresh Room",
				UserName:   "Fresh User",
				AlarmTypes: domain.AlarmTypes{domain.AlarmTypeLive},
			},
		}, nil
	}
	loadMemberNamesFromRepository = func(context.Context, *Repository) (map[string]string, error) {
		return map[string]string{"UC_FRESH": "Fresh Member"}, nil
	}
	t.Cleanup(func() {
		loadAllAlarmsFromRepository = originalLoader
		loadMemberNamesFromRepository = originalMemberNameLoader
	})

	_, err := cacheSvc.SAdd(ctx, sharedalarmkeys.BuildRoomAlarmKey("room-orphan"), []string{"UC_ORPHAN"})
	require.NoError(t, err)
	require.NoError(t, cacheSvc.Set(ctx, contractsalarm.DispatchQueueKey, "queue-marker", 0))
	require.NoError(t, cacheSvc.Set(ctx, "alarm:chzzk_channels", "mapping-marker", 0))
	require.NoError(t, cacheSvc.Set(ctx, "alarm:next_stream:UC_KEEP", "stream-marker", 0))

	summary, err := RebuildSubscriberCacheFromRepository(ctx, cacheSvc, &Repository{})
	require.NoError(t, err)
	assert.Equal(t, CacheWarmSummary{AlarmCount: 1, RoomCount: 1, ChannelCount: 1}, summary)

	orphanRoomChannels, err := cacheSvc.SMembers(ctx, sharedalarmkeys.BuildRoomAlarmKey("room-orphan"))
	require.NoError(t, err)
	assert.Empty(t, orphanRoomChannels)

	dispatchQueueExists, err := cacheSvc.Exists(ctx, contractsalarm.DispatchQueueKey)
	require.NoError(t, err)
	assert.True(t, dispatchQueueExists)

	chzzkMapExists, err := cacheSvc.Exists(ctx, "alarm:chzzk_channels")
	require.NoError(t, err)
	assert.True(t, chzzkMapExists)

	nextStreamExists, err := cacheSvc.Exists(ctx, "alarm:next_stream:UC_KEEP")
	require.NoError(t, err)
	assert.True(t, nextStreamExists)
}

type countingWarmCacheClient struct {
	cache.Client
	sAddCalls  int
	hSetCalls  int
	hmSetCalls int
}

func (c *countingWarmCacheClient) SAdd(ctx context.Context, key string, members []string) (int64, error) {
	c.sAddCalls++
	return c.Client.SAdd(ctx, key, members)
}

func (c *countingWarmCacheClient) HSet(ctx context.Context, key, field, value string) error {
	c.hSetCalls++
	return c.Client.HSet(ctx, key, field, value)
}

func (c *countingWarmCacheClient) HMSet(ctx context.Context, key string, fields map[string]any) error {
	c.hmSetCalls++
	return c.Client.HMSet(ctx, key, fields)
}

func newMemoryCacheClient(t *testing.T) cache.Client {
	t.Helper()

	mini := miniredis.RunT(t)
	rawClient, err := valkey.NewClient(valkey.ClientOption{
		InitAddress:       []string{mini.Addr()},
		DisableCache:      true,
		ForceSingleClient: true,
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	if err := rawClient.Do(ctx, rawClient.B().Ping().Build()).Error(); err != nil {
		rawClient.Close()
		mini.Close()
		t.Fatalf("Ping() error = %v", err)
	}

	client := cachemocks.NewStrictClient()
	client.CloseFunc = func() error {
		rawClient.Close()
		return nil
	}
	client.GetClientFunc = func() valkey.Client { return rawClient }
	client.BFunc = rawClient.B
	client.BuilderFunc = rawClient.B
	client.DoMultiFunc = func(ctx context.Context, cmds ...valkey.Completed) []valkey.ValkeyResult {
		return rawClient.DoMulti(ctx, cmds...)
	}
	client.SAddFunc = func(ctx context.Context, key string, members []string) (int64, error) {
		if len(members) == 0 {
			return 0, nil
		}

		resp := rawClient.Do(ctx, rawClient.B().Sadd().Key(key).Member(members...).Build())
		if resp.Error() != nil {
			return 0, resp.Error()
		}

		return resp.AsInt64()
	}
	client.SMembersFunc = func(ctx context.Context, key string) ([]string, error) {
		resp := rawClient.Do(ctx, rawClient.B().Smembers().Key(key).Build())
		if resp.Error() != nil {
			return nil, resp.Error()
		}

		return resp.AsStrSlice()
	}
	client.HSetFunc = func(ctx context.Context, key, field, value string) error {
		return rawClient.Do(ctx, rawClient.B().Hset().Key(key).FieldValue().FieldValue(field, value).Build()).Error()
	}
	client.HMSetFunc = func(ctx context.Context, key string, fields map[string]any) error {
		if len(fields) == 0 {
			return nil
		}

		builder := rawClient.B().Hset().Key(key).FieldValue()
		for field, value := range fields {
			builder = builder.FieldValue(field, fmt.Sprintf("%v", value))
		}

		return rawClient.Do(ctx, builder.Build()).Error()
	}
	client.HGetFunc = func(ctx context.Context, key, field string) (string, error) {
		resp := rawClient.Do(ctx, rawClient.B().Hget().Key(key).Field(field).Build())
		if util.IsValkeyNil(resp.Error()) {
			return "", nil
		}
		if resp.Error() != nil {
			return "", resp.Error()
		}

		return resp.ToString()
	}
	client.SetFunc = func(ctx context.Context, key string, value any, ttl time.Duration) error {
		builder := rawClient.B().Set().Key(key).Value(fmt.Sprintf("%v", value))
		if ttl > 0 {
			return rawClient.Do(ctx, builder.ExSeconds(int64(ttl.Seconds())).Build()).Error()
		}

		return rawClient.Do(ctx, builder.Build()).Error()
	}
	client.ExistsFunc = func(ctx context.Context, key string) (bool, error) {
		resp := rawClient.Do(ctx, rawClient.B().Exists().Key(key).Build())
		if resp.Error() != nil {
			return false, resp.Error()
		}

		count, err := resp.AsInt64()
		if err != nil {
			return false, err
		}

		return count > 0, nil
	}
	client.ScanKeysFunc = func(ctx context.Context, pattern string, batchSize int64) ([]string, error) {
		if batchSize <= 0 {
			batchSize = 100
		}

		var keys []string
		cursor := uint64(0)

		for {
			resp := rawClient.Do(ctx, rawClient.B().Scan().Cursor(cursor).Match(pattern).Count(batchSize).Build())
			if resp.Error() != nil {
				return nil, resp.Error()
			}

			entry, err := resp.AsScanEntry()
			if err != nil {
				return nil, err
			}

			keys = append(keys, entry.Elements...)
			cursor = entry.Cursor
			if cursor == 0 {
				return keys, nil
			}
		}
	}
	client.DelManyFunc = func(ctx context.Context, keys []string) (int64, error) {
		if len(keys) == 0 {
			return 0, nil
		}

		resp := rawClient.Do(ctx, rawClient.B().Del().Key(keys...).Build())
		if resp.Error() != nil {
			return 0, resp.Error()
		}

		return resp.AsInt64()
	}
	client.DelFunc = func(ctx context.Context, key string) error {
		return rawClient.Do(ctx, rawClient.B().Del().Key(key).Build()).Error()
	}

	t.Cleanup(func() {
		_ = client.Close()
		mini.Close()
	})

	return client
}
