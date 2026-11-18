package alarmstore

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	alarmkeys "github.com/kapu/hololive-alarm-worker/internal/service/alarm/keys"
	"github.com/kapu/hololive-shared/pkg/domain"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
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
	requireAlarmRecord(t, db, &domain.Alarm{RoomID: "room-a-default", ChannelID: uncachedChannelA, AlarmTypes: domain.DefaultAlarmTypes})
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

	warmed, err := cacheClient.SMembers(ctx, alarmkeys.BuildChannelSubscriberKey(uncachedChannelA, domain.AlarmTypeLive))
	require.NoError(t, err)
	require.Empty(t, warmed)

	got, err = resolver.ResolveUncachedChannelSubscribersByType(ctx, []string{uncachedChannelC}, domain.AlarmTypeLive)
	require.NoError(t, err)
	require.Empty(t, got)
	require.Equal(t, 2, queries)
}

func TestResolveUncachedChannelSubscribersByType_RequiresDatabase(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	cacheOnly := NewSubscriberResolver(testutil.NewTestCacheService(ctx, t), nil)

	got, err := cacheOnly.ResolveUncachedChannelSubscribersByType(ctx, []string{"", " "}, domain.AlarmTypeLive)
	require.NoError(t, err)
	require.Empty(t, got)

	_, err = cacheOnly.ResolveUncachedChannelSubscribersByType(ctx, []string{"UC_evicted"}, domain.AlarmTypeLive)
	require.ErrorContains(t, err, "database is nil")
}

func TestResolveUncachedChannelSubscribersByType_ResolvesAllChannelsInOneQueryWithoutCache(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	db := newAlarmTargetLookupTestDB(t)

	const channelCount = 100

	channelIDs := make([]string, 0, channelCount*2)
	wantRooms := map[string][]string{}

	for i := range channelCount {
		channelID := fmt.Sprintf("UC_uncached_%03d", i)

		channelIDs = append(channelIDs, channelID)

		if i%10 == 0 {
			channelIDs = append(channelIDs, " "+channelID+" ", channelID, "")
		}

		if i%2 == 0 {
			continue
		}

		roomID := "room-" + channelID

		wantRooms[channelID] = []string{roomID}

		requireAlarmRecord(t, db, &domain.Alarm{RoomID: roomID, ChannelID: channelID, AlarmTypes: domain.AlarmTypes{domain.AlarmTypeLive}})
	}

	queries := 0

	registerAlarmQueryHook(t, db, func() { queries++ })

	got, err := NewSubscriberResolver(cachemocks.NewStrictClient(), db).ResolveUncachedChannelSubscribersByType(ctx, channelIDs, domain.AlarmTypeLive)
	require.NoError(t, err)
	require.Equal(t, wantRooms, got)
	require.Equal(t, 1, queries)
}
