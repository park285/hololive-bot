package alarm

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
	sharedalarmkeys "github.com/kapu/hololive-shared/pkg/service/alarm/keys"
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

	got, err := ResolveUncachedChannelSubscribersByType(
		ctx, cacheClient, db, []string{uncachedChannelA, " " + uncachedChannelB + " ", uncachedChannelC, uncachedChannelA}, domain.AlarmTypeLive,
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

	// LIVE 구독이 없는 채널은 empty marker로 기록되어 다음 조회는 DB를 거치지 않는다.
	knownEmpty, err := cacheClient.Exists(ctx, sharedalarmkeys.BuildChannelSubscriberEmptyKey(uncachedChannelC, domain.AlarmTypeLive))
	require.NoError(t, err)
	require.True(t, knownEmpty)

	got, err = ResolveUncachedChannelSubscribersByType(ctx, cacheClient, db, []string{uncachedChannelC}, domain.AlarmTypeLive)
	require.NoError(t, err)
	require.Empty(t, got)
	require.Equal(t, 1, queries)
}

func TestResolveUncachedChannelSubscribersByType_KnownEmptyWithoutDatabase(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	cacheClient := testutil.NewTestCacheService(ctx, t)
	require.NoError(t, cacheClient.Set(ctx, sharedalarmkeys.BuildChannelSubscriberEmptyKey("UC_known_empty", domain.AlarmTypeLive), "1", time.Minute))

	got, err := ResolveUncachedChannelSubscribersByType(ctx, cacheClient, nil, []string{"UC_known_empty"}, domain.AlarmTypeLive)
	require.NoError(t, err)
	require.Empty(t, got)

	_, err = ResolveUncachedChannelSubscribersByType(ctx, cacheClient, nil, []string{"UC_known_empty", "UC_evicted"}, domain.AlarmTypeLive)
	require.ErrorContains(t, err, "database is nil")
}
