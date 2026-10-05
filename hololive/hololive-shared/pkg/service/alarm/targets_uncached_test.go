package alarm

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/valkey-io/valkey-go"

	"github.com/kapu/hololive-shared/pkg/domain"
	sharedalarmkeys "github.com/kapu/hololive-shared/pkg/service/alarm/keys"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/testutil"
)

const (
	uncachedChannelA = "UC_uncached_a"
	uncachedChannelB = "UC_uncached_b"
	uncachedChannelC = "UC_uncached_c"
)

func TestResolveUncachedChannelSubscribersByType_RecoversChannelsInOneQuery(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	db := newAlarmTargetLookupTestDB(t)
	cacheClient := testutil.NewTestCacheService(ctx, t)

	requireAlarmRecord(t, db, &domain.Alarm{RoomID: "room-a-live", ChannelID: uncachedChannelA, AlarmTypes: domain.AlarmTypes{domain.AlarmTypeLive}})
	requireAlarmRecord(t, db, &domain.Alarm{RoomID: "room-a-default", ChannelID: uncachedChannelA})
	requireAlarmRecord(t, db, &domain.Alarm{RoomID: "room-a-community", ChannelID: uncachedChannelA, AlarmTypes: domain.AlarmTypes{domain.AlarmTypeCommunity}})
	requireAlarmRecord(t, db, &domain.Alarm{RoomID: "room-b-live", ChannelID: uncachedChannelB, AlarmTypes: domain.AlarmTypes{domain.AlarmTypeLive}})
	requireAlarmRecord(t, db, &domain.Alarm{RoomID: "room-c-community", ChannelID: uncachedChannelC, AlarmTypes: domain.AlarmTypes{domain.AlarmTypeCommunity}})

	queries := 0

	registerAlarmQueryHook(t, db, func() { queries++ })

	resolver := NewSubscriberResolver(cacheClient, db)
	got, err := resolver.ResolveUncachedChannelSubscribersByType(
		ctx, []string{uncachedChannelA, " " + uncachedChannelB + " ", uncachedChannelC, uncachedChannelA}, domain.AlarmTypeLive,
	)
	require.NoError(t, err)
	require.Equal(t, 1, queries, "evicted channels must be recovered by a single batched query, not one query per channel")
	require.Len(t, got, 2)
	require.ElementsMatch(t, []string{"room-a-live", "room-a-default"}, got[uncachedChannelA])
	require.Equal(t, []string{"room-b-live"}, got[uncachedChannelB])

	// read-through: 조회 뒤 SADD가 동시 구독 해지의 SREM을 되돌리지 않도록 set은 다시 채우지 않는다.
	warmed, err := cacheClient.SMembers(ctx, sharedalarmkeys.BuildChannelSubscriberKey(uncachedChannelA, domain.AlarmTypeLive))
	require.NoError(t, err)
	require.Empty(t, warmed)

	// 구독 없는 조회도 캐시를 쓰지 않으며 다음 조회에서 DB를 다시 확인한다.
	knownEmpty, err := cacheClient.Exists(ctx, sharedalarmkeys.BuildChannelSubscriberEmptyKey(uncachedChannelC, domain.AlarmTypeLive))
	require.NoError(t, err)
	require.False(t, knownEmpty)

	got, err = resolver.ResolveUncachedChannelSubscribersByType(ctx, []string{uncachedChannelC}, domain.AlarmTypeLive)
	require.NoError(t, err)
	require.Empty(t, got)
	require.Equal(t, 2, queries)
}

func TestResolveUncachedChannelSubscribersByType_KnownEmptyWithoutDatabase(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	cacheClient := testutil.NewTestCacheService(ctx, t)
	require.NoError(t, cacheClient.Set(ctx, sharedalarmkeys.BuildChannelSubscriberEmptyKey("UC_known_empty", domain.AlarmTypeLive), "1", time.Minute))

	cacheOnly := NewSubscriberResolver(cacheClient, nil)
	got, err := cacheOnly.ResolveUncachedChannelSubscribersByType(ctx, []string{"UC_known_empty"}, domain.AlarmTypeLive)
	require.NoError(t, err)
	require.Empty(t, got)

	_, err = cacheOnly.ResolveUncachedChannelSubscribersByType(ctx, []string{"UC_known_empty", "UC_evicted"}, domain.AlarmTypeLive)
	require.ErrorContains(t, err, "database is nil")
}

// markerProbeCache는 empty marker 확인이 단건 EXISTS인지 pipeline인지 세고, 필요하면 pipeline 결과를 바꾼다.
type markerProbeCache struct {
	cache.Client

	existsCalls  int
	doMultiCalls int
	commands     int
	results      func(cmds []valkey.Completed) []valkey.ValkeyResult
}

func (c *markerProbeCache) Exists(ctx context.Context, key string) (bool, error) {
	c.existsCalls++

	exists, err := c.Client.Exists(ctx, key)
	if err != nil {
		return false, fmt.Errorf("exists: %w", err)
	}

	return exists, nil
}

func (c *markerProbeCache) DoMulti(ctx context.Context, cmds ...valkey.Completed) []valkey.ValkeyResult {
	c.doMultiCalls++

	c.commands += len(cmds)

	if c.results != nil {
		return c.results(cmds)
	}

	return c.Client.DoMulti(ctx, cmds...)
}

// 100개 채널의 empty marker는 한 번의 pipeline으로 확인하고, 결과는 입력 채널 위치와 정확히 대응해야 한다.
// 모든 채널에 DB 구독이 있으므로 marker가 다른 채널에 잘못 대응되면 결과 집합이 달라진다.
func TestResolveUncachedChannelSubscribersByType_PipelinesEmptyMarkerProbes(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	db := newAlarmTargetLookupTestDB(t)
	baseCache := testutil.NewTestCacheService(ctx, t)
	probeCache := &markerProbeCache{Client: baseCache}

	const channelCount = 100

	channelIDs := make([]string, 0, channelCount*2)
	wantRooms := map[string][]string{}

	for i := range channelCount {
		channelID := fmt.Sprintf("UC_marker_%03d", i)
		roomID := "room-" + channelID
		requireAlarmRecord(t, db, &domain.Alarm{RoomID: roomID, ChannelID: channelID, AlarmTypes: domain.AlarmTypes{domain.AlarmTypeLive}})

		if i%2 == 0 {
			require.NoError(t, baseCache.Set(ctx, sharedalarmkeys.BuildChannelSubscriberEmptyKey(channelID, domain.AlarmTypeLive), "1", time.Minute))
		} else {
			wantRooms[channelID] = []string{roomID}
		}

		channelIDs = append(channelIDs, channelID)

		if i%10 == 0 {
			channelIDs = append(channelIDs, " "+channelID+" ", channelID, "")
		}
	}

	queries := 0

	registerAlarmQueryHook(t, db, func() { queries++ })

	got, err := NewSubscriberResolver(probeCache, db).ResolveUncachedChannelSubscribersByType(ctx, channelIDs, domain.AlarmTypeLive)
	require.NoError(t, err)
	require.Equal(t, wantRooms, got)
	require.Zero(t, probeCache.existsCalls, "marker probes must not fall back to serial EXISTS")
	require.Equal(t, 1, probeCache.doMultiCalls)
	require.Equal(t, channelCount, probeCache.commands, "duplicate and blank channels must not be probed")
	require.Equal(t, 1, queries)
}

// marker 확인이 실패하면 해당 채널을 구독 0으로 단정하지 않는다. DB가 있으면 DB로 확정하고, 없으면 오류를 반환한다.
func TestResolveUncachedChannelSubscribersByType_MarkerProbeFailureIsNotKnownEmpty(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	db := newAlarmTargetLookupTestDB(t)
	baseCache := testutil.NewTestCacheService(ctx, t)
	probeErr := errors.New("marker probe failed")

	channels := []string{"UC_probe_fail_a", "UC_probe_fail_b"}
	for _, channelID := range channels {
		requireAlarmRecord(t, db, &domain.Alarm{RoomID: "room-" + channelID, ChannelID: channelID, AlarmTypes: domain.AlarmTypes{domain.AlarmTypeLive}})
		require.NoError(t, baseCache.Set(ctx, sharedalarmkeys.BuildChannelSubscriberEmptyKey(channelID, domain.AlarmTypeLive), "1", time.Minute))
	}

	for name, results := range map[string]func([]valkey.Completed) []valkey.ValkeyResult{
		"per-command error": func(cmds []valkey.Completed) []valkey.ValkeyResult {
			out := make([]valkey.ValkeyResult, len(cmds))
			for i := range out {
				out[i] = valkey.NewErrorResult(probeErr)
			}

			return out
		},
		"missing results": func([]valkey.Completed) []valkey.ValkeyResult { return nil },
	} {
		probeCache := &markerProbeCache{Client: baseCache, results: results}

		got, err := NewSubscriberResolver(probeCache, db).ResolveUncachedChannelSubscribersByType(ctx, channels, domain.AlarmTypeLive)
		require.NoError(t, err, name)
		require.Equal(t, map[string][]string{
			channels[0]: {"room-" + channels[0]},
			channels[1]: {"room-" + channels[1]},
		}, got, name)

		_, err = NewSubscriberResolver(probeCache, nil).ResolveUncachedChannelSubscribersByType(ctx, channels, domain.AlarmTypeLive)
		require.ErrorContains(t, err, "check empty subscriber cache", name)
	}
}
