package alarm

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/valkey-io/valkey-go"

	"github.com/kapu/hololive-shared/pkg/domain"
	sharedalarmkeys "github.com/kapu/hololive-shared/pkg/service/alarm/keys"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
	"github.com/kapu/hololive-shared/pkg/util"
)

const (
	testWarmChannelA = "UC_A"
	testWarmChannelB = "UC_B"
)

func typeSpecificWarmAlarms() []*domain.Alarm {
	return []*domain.Alarm{
		{
			RoomID:     testCommunityRoomID,
			UserID:     "user-community",
			ChannelID:  testWarmChannelA,
			MemberName: "Member A",
			RoomName:   "Community Room",
			UserName:   "Community User",
			AlarmTypes: domain.AlarmTypes{domain.AlarmTypeCommunity},
		},
		{
			RoomID:     "room-shorts",
			UserID:     "user-shorts",
			ChannelID:  testWarmChannelA,
			MemberName: "Member A",
			RoomName:   "Shorts Room",
			UserName:   "Shorts User",
			AlarmTypes: domain.AlarmTypes{domain.AlarmTypeShorts},
		},
		{
			RoomID:     testDefaultRoomID,
			UserID:     "user-default",
			ChannelID:  testWarmChannelB,
			MemberName: "Member B",
			RoomName:   "Default Room",
			UserName:   "Default User",
		},
	}
}

// stubRebuildLoaders는 rebuild가 읽을 PG 스냅샷을 고정한다. 전역 loader를 바꾸므로 호출하는 테스트는 병렬로 돌리지 않는다.
func stubRebuildLoaders(t *testing.T, alarms []*domain.Alarm, alarmErr error, memberNames map[string]string, memberErr error) {
	t.Helper()

	originalLoader := loadAllAlarmsFromRepository
	originalMemberNameLoader := loadMemberNamesFromRepository

	loadAllAlarmsFromRepository = func(context.Context, *Repository) ([]*domain.Alarm, error) {
		if alarmErr != nil {
			return nil, alarmErr
		}

		return alarms, nil
	}
	loadMemberNamesFromRepository = func(context.Context, *Repository) (map[string]string, error) {
		if memberErr != nil {
			return nil, memberErr
		}

		return memberNames, nil
	}

	t.Cleanup(func() {
		loadAllAlarmsFromRepository = originalLoader
		loadMemberNamesFromRepository = originalMemberNameLoader
	})
}

func TestRebuildSubscriberCacheFromRepository_WritesOnlyTypeSpecificSubscriberCache(t *testing.T) {
	ctx := t.Context()
	cacheClient := newMemoryCacheClient(t)
	stubRebuildLoaders(t, typeSpecificWarmAlarms(), nil, nil, nil)

	summary, err := RebuildSubscriberCacheFromRepository(ctx, cacheClient, &Repository{})
	require.NoError(t, err)
	assert.Equal(t, CacheWarmSummary{AlarmCount: 3, RoomCount: 3, ChannelCount: 2}, summary)

	channelRegistry, err := cacheClient.SMembers(ctx, sharedalarmkeys.AlarmChannelRegistryKey)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{testWarmChannelA, testWarmChannelB}, channelRegistry)

	for _, tc := range []struct {
		channelID string
		alarmType domain.AlarmType
		want      []string
	}{
		{testWarmChannelA, domain.AlarmTypeCommunity, []string{testCommunityRoomID}},
		{testWarmChannelA, domain.AlarmTypeShorts, []string{"room-shorts"}},
		{testWarmChannelA, domain.AlarmTypeLive, nil},
		{testWarmChannelB, domain.AlarmTypeLive, []string{testDefaultRoomID}},
		{testWarmChannelB, domain.AlarmTypeCommunity, []string{testDefaultRoomID}},
		{testWarmChannelB, domain.AlarmTypeShorts, []string{testDefaultRoomID}},
	} {
		subscribers, membersErr := cacheClient.SMembers(ctx, sharedalarmkeys.BuildChannelSubscriberKey(tc.channelID, tc.alarmType))
		require.NoError(t, membersErr)
		assert.ElementsMatch(t, tc.want, subscribers, "%s %s", tc.channelID, tc.alarmType)
	}

	memberName, err := cacheClient.HGet(ctx, sharedalarmkeys.MemberNameKey, testWarmChannelA)
	require.NoError(t, err)
	assert.Equal(t, "Member A", memberName)

	// 방 목록·방 이름·사용자 이름은 PG가 원천이라 rebuild가 방 단위 index나 이름 hash를 만들지 않는다.
	written, err := cacheClient.ScanKeys(ctx, "*", cacheScanBatchSize)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{
		sharedalarmkeys.AlarmChannelRegistryKey,
		sharedalarmkeys.MemberNameKey,
		sharedalarmkeys.BuildChannelSubscriberKey(testWarmChannelA, domain.AlarmTypeCommunity),
		sharedalarmkeys.BuildChannelSubscriberKey(testWarmChannelA, domain.AlarmTypeShorts),
		sharedalarmkeys.BuildChannelSubscriberKey(testWarmChannelB, domain.AlarmTypeLive),
		sharedalarmkeys.BuildChannelSubscriberKey(testWarmChannelB, domain.AlarmTypeCommunity),
		sharedalarmkeys.BuildChannelSubscriberKey(testWarmChannelB, domain.AlarmTypeShorts),
	}, written)
}

func TestRebuildSubscriberCacheFromRepository_MarksEmptyCacheState(t *testing.T) {
	ctx := t.Context()
	cacheClient := newMemoryCacheClient(t)
	stubRebuildLoaders(t, nil, nil, nil, nil)

	summary, err := RebuildSubscriberCacheFromRepository(ctx, cacheClient, &Repository{})
	require.NoError(t, err)
	assert.Equal(t, CacheWarmSummary{}, summary)

	emptyMarkerExists, err := cacheClient.Exists(ctx, sharedalarmkeys.AlarmSubscriberCacheEmptyKey)
	require.NoError(t, err)
	assert.True(t, emptyMarkerExists)

	channelRegistryExists, err := cacheClient.Exists(ctx, sharedalarmkeys.AlarmChannelRegistryKey)
	require.NoError(t, err)
	assert.False(t, channelRegistryExists)
}

func TestRebuildSubscriberCacheFromRepository_ClearsEmptyCacheMarkerWhenAlarmsExist(t *testing.T) {
	ctx := t.Context()
	cacheClient := newMemoryCacheClient(t)
	require.NoError(t, cacheClient.Set(ctx, sharedalarmkeys.AlarmSubscriberCacheEmptyKey, "1", 0))
	stubRebuildLoaders(t, []*domain.Alarm{{RoomID: "room-1", UserID: "user-1", ChannelID: "UC_ONE"}}, nil, nil, nil)

	_, err := RebuildSubscriberCacheFromRepository(ctx, cacheClient, &Repository{})
	require.NoError(t, err)

	emptyMarkerExists, err := cacheClient.Exists(ctx, sharedalarmkeys.AlarmSubscriberCacheEmptyKey)
	require.NoError(t, err)
	assert.False(t, emptyMarkerExists)
}

func TestRebuildSubscriberCacheFromRepository_UsesBatchedWrites(t *testing.T) {
	ctx := t.Context()
	baseCache := newMemoryCacheClient(t)
	countingCache := &countingWarmCacheClient{Client: baseCache}

	alarms := make([]*domain.Alarm, 0, 48)

	for i := range 48 {
		alarms = append(alarms, &domain.Alarm{
			RoomID:     "room-" + strconv.Itoa(i),
			UserID:     "user-" + strconv.Itoa(i),
			ChannelID:  "UC_BATCH",
			MemberName: "Member " + strconv.Itoa(i),
			RoomName:   "Room " + strconv.Itoa(i),
			UserName:   "User " + strconv.Itoa(i),
		})
	}

	stubRebuildLoaders(t, alarms, nil, nil, nil)

	summary, err := RebuildSubscriberCacheFromRepository(ctx, countingCache, &Repository{})
	require.NoError(t, err)
	assert.Equal(t, CacheWarmSummary{AlarmCount: 48, RoomCount: 48, ChannelCount: 1}, summary)
	assert.Less(t, countingCache.sAddCalls, len(alarms)*(1+len(domain.DefaultAlarmTypes)))
	assert.Zero(t, countingCache.hSetCalls)
	assert.Equal(t, 1, countingCache.hmSetCalls)

	liveSubscribers, err := countingCache.SMembers(ctx, sharedalarmkeys.BuildChannelSubscriberKey("UC_BATCH", domain.AlarmTypeLive))
	require.NoError(t, err)
	assert.Len(t, liveSubscribers, len(alarms))
}

func TestRebuildSubscriberCacheFromRepository_UsesAuthoritativeMemberNames(t *testing.T) {
	ctx := t.Context()
	cacheClient := newMemoryCacheClient(t)
	stubRebuildLoaders(t, []*domain.Alarm{
		{
			RoomID:     "room-1",
			UserID:     "user-1",
			ChannelID:  "UC_RADEN",
			MemberName: "Juufuutei Raden",
			AlarmTypes: domain.AlarmTypes{domain.AlarmTypeLive},
		},
	}, nil, map[string]string{"UC_RADEN": "라덴"}, nil)

	_, err := RebuildSubscriberCacheFromRepository(ctx, cacheClient, &Repository{})
	require.NoError(t, err)

	memberName, err := cacheClient.HGet(ctx, sharedalarmkeys.MemberNameKey, "UC_RADEN")
	require.NoError(t, err)
	assert.Equal(t, "라덴", memberName)
}

func TestRebuildSubscriberCacheFromRepository_LoadError(t *testing.T) {
	stubRebuildLoaders(t, nil, errors.New("load failed"), nil, nil)

	_, err := RebuildSubscriberCacheFromRepository(t.Context(), newMemoryCacheClient(t), &Repository{})
	require.ErrorContains(t, err, "rebuild subscriber cache from repository: load alarms")
	assert.ErrorContains(t, err, "load failed")
}

func TestRebuildSubscriberCacheFromRepository_MemberNameLoadErrorLeavesCacheCleared(t *testing.T) {
	ctx := t.Context()
	cacheClient := newMemoryCacheClient(t)
	stubRebuildLoaders(t, []*domain.Alarm{
		{
			RoomID:     testFreshRoomID,
			UserID:     "user-fresh",
			ChannelID:  testFreshChannelID,
			MemberName: testFreshMemberName,
			AlarmTypes: domain.AlarmTypes{domain.AlarmTypeLive},
		},
	}, nil, nil, errors.New("member names unavailable"))

	_, err := cacheClient.SAdd(ctx, sharedalarmkeys.AlarmChannelRegistryKey, []string{testExistingChannel})
	require.NoError(t, err)

	_, err = cacheClient.SAdd(ctx, sharedalarmkeys.BuildChannelSubscriberKey(testExistingChannel, domain.AlarmTypeLive), []string{testExistingRoomID})
	require.NoError(t, err)
	require.NoError(t, cacheClient.HSet(ctx, sharedalarmkeys.MemberNameKey, testExistingChannel, "Existing Member"))

	_, err = RebuildSubscriberCacheFromRepository(ctx, cacheClient, &Repository{})
	require.ErrorContains(t, err, "rebuild subscriber cache from repository: load member names")

	channelRegistry, err := cacheClient.SMembers(ctx, sharedalarmkeys.AlarmChannelRegistryKey)
	require.NoError(t, err)
	assert.Empty(t, channelRegistry)

	existingSubscribers, err := cacheClient.SMembers(ctx, sharedalarmkeys.BuildChannelSubscriberKey(testExistingChannel, domain.AlarmTypeLive))
	require.NoError(t, err)
	assert.Empty(t, existingSubscribers)
}

func TestCompactUniqueStrings_TrimsDedupesAndPreservesOrder(t *testing.T) {
	t.Parallel()

	values := []string{" room-1 ", "", "room-2", "room-1", " room-3 ", "room-2", "room-4"}

	assert.Equal(t, []string{"room-1", "room-2", "room-3", "room-4"}, compactUniqueStrings(values))
}

func seedStaleSubscriberCache(t *testing.T, cacheClient cache.Client) {
	t.Helper()

	ctx := t.Context()

	_, err := cacheClient.SAdd(ctx, sharedalarmkeys.AlarmChannelRegistryKey, []string{"UC_STALE"})
	require.NoError(t, err)

	_, err = cacheClient.SAdd(ctx, sharedalarmkeys.BuildChannelSubscriberKey("UC_STALE", domain.AlarmTypeLive), []string{"room-stale"})
	require.NoError(t, err)
	require.NoError(t, cacheClient.HSet(ctx, sharedalarmkeys.MemberNameKey, "UC_STALE", "Stale Member"))
	require.NoError(t, cacheClient.Set(ctx, sharedalarmkeys.BuildChannelSubscriberEmptyKey("UC_STALE", domain.AlarmTypeLive), "1", time.Minute))
}

func TestRebuildSubscriberCacheFromRepository_ReplacesStaleCacheState(t *testing.T) {
	ctx := t.Context()
	cacheClient := newMemoryCacheClient(t)
	stubRebuildLoaders(t, []*domain.Alarm{
		{
			RoomID:     testFreshRoomID,
			UserID:     "user-fresh",
			ChannelID:  testFreshChannelID,
			MemberName: testFreshMemberName,
			AlarmTypes: domain.AlarmTypes{domain.AlarmTypeCommunity},
		},
	}, nil, map[string]string{testFreshChannelID: testFreshMemberName}, nil)

	seedStaleSubscriberCache(t, cacheClient)

	summary, err := RebuildSubscriberCacheFromRepository(ctx, cacheClient, &Repository{})
	require.NoError(t, err)
	assert.Equal(t, CacheWarmSummary{AlarmCount: 1, RoomCount: 1, ChannelCount: 1}, summary)

	channelRegistry, err := cacheClient.SMembers(ctx, sharedalarmkeys.AlarmChannelRegistryKey)
	require.NoError(t, err)
	assert.Equal(t, []string{testFreshChannelID}, channelRegistry)

	staleLiveSubscribers, err := cacheClient.SMembers(ctx, sharedalarmkeys.BuildChannelSubscriberKey("UC_STALE", domain.AlarmTypeLive))
	require.NoError(t, err)
	assert.Empty(t, staleLiveSubscribers)

	staleEmptyKnown, err := cacheClient.Exists(ctx, sharedalarmkeys.BuildChannelSubscriberEmptyKey("UC_STALE", domain.AlarmTypeLive))
	require.NoError(t, err)
	assert.False(t, staleEmptyKnown)

	staleMemberName, err := cacheClient.HGet(ctx, sharedalarmkeys.MemberNameKey, "UC_STALE")
	require.NoError(t, err)
	assert.Empty(t, staleMemberName)

	freshCommunitySubscribers, err := cacheClient.SMembers(ctx, sharedalarmkeys.BuildChannelSubscriberKey(testFreshChannelID, domain.AlarmTypeCommunity))
	require.NoError(t, err)
	assert.Equal(t, []string{testFreshRoomID}, freshCommunitySubscribers)
}

func TestRebuildSubscriberCacheFromRepository_PreservesNonSubscriberAlarmKeys(t *testing.T) {
	ctx := t.Context()
	cacheClient := newMemoryCacheClient(t)
	stubRebuildLoaders(t, []*domain.Alarm{
		{RoomID: testFreshRoomID, UserID: "user-fresh", ChannelID: testFreshChannelID, AlarmTypes: domain.AlarmTypes{domain.AlarmTypeLive}},
	}, nil, nil, nil)

	require.NoError(t, cacheClient.Set(ctx, "alarm:dispatch:wakeup:guard", "wakeup-marker", 0))
	require.NoError(t, cacheClient.Set(ctx, "alarm:next_stream:UC_KEEP", "stream-marker", 0))

	_, err := RebuildSubscriberCacheFromRepository(ctx, cacheClient, &Repository{})
	require.NoError(t, err)

	wakeupGuardExists, err := cacheClient.Exists(ctx, "alarm:dispatch:wakeup:guard")
	require.NoError(t, err)
	assert.True(t, wakeupGuardExists)

	nextStreamExists, err := cacheClient.Exists(ctx, "alarm:next_stream:UC_KEEP")
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

	out, err := c.Client.SAdd(ctx, key, members)
	if err != nil {
		return out, fmt.Errorf("s add: %w", err)
	}

	return out, nil
}

func (c *countingWarmCacheClient) HSet(ctx context.Context, key, field, value string) error {
	c.hSetCalls++
	if err := c.Client.HSet(ctx, key, field, value); err != nil {
		return fmt.Errorf("h set: %w", err)
	}

	return nil
}

func (c *countingWarmCacheClient) HMSet(ctx context.Context, key string, fields map[string]any) error {
	c.hmSetCalls++
	if err := c.Client.HMSet(ctx, key, fields); err != nil {
		return fmt.Errorf("HM set: %w", err)
	}

	return nil
}

func newMemoryCacheClient(t *testing.T) cache.Client {
	t.Helper()

	mini, rawClient := newMemoryValkeyClient(t)
	client := cachemocks.NewStrictClient()
	configureMemoryCacheCore(client, rawClient)
	configureMemoryCacheSets(client, rawClient)
	configureMemoryCacheHashes(client, rawClient)
	configureMemoryCacheStrings(client, rawClient)
	configureMemoryCacheKeys(client, rawClient)

	t.Cleanup(func() {
		if err := client.Close(); err != nil {
			t.Errorf("close cache client: %v", err)
		}

		mini.Close()
	})

	return client
}

func newMemoryValkeyClient(t *testing.T) (*miniredis.Miniredis, valkey.Client) {
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

	return mini, rawClient
}

func configureMemoryCacheCore(client *cachemocks.Client, rawClient valkey.Client) {
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
}

func configureMemoryCacheSets(client *cachemocks.Client, rawClient valkey.Client) {
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
}

func configureMemoryCacheHashes(client *cachemocks.Client, rawClient valkey.Client) {
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
}

func configureMemoryCacheStrings(client *cachemocks.Client, rawClient valkey.Client) {
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
			return false, fmt.Errorf("as int64: %w", err)
		}

		return count > 0, nil
	}
}

func configureMemoryCacheKeys(client *cachemocks.Client, rawClient valkey.Client) {
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
				return nil, fmt.Errorf("as scan entry: %w", err)
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
}
