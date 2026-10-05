package subscriptions

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
	sharedalarm "github.com/kapu/hololive-shared/pkg/service/alarm"
	sharedalarmkeys "github.com/kapu/hololive-shared/pkg/service/alarm/keys"
	databasemocks "github.com/kapu/hololive-shared/pkg/service/database/mocks"
	sharedtestutil "github.com/kapu/hololive-shared/pkg/testutil"
)

func newEvictionAlarmService(t *testing.T) (*AlarmService, *pgxpool.Pool) {
	t.Helper()

	pool := dbtest.NewPool(t)
	logger := newDiscardAlarmLogger()
	repository := sharedalarm.NewRepository(&databasemocks.Client{GetPoolFunc: func() *pgxpool.Pool { return pool }}, logger)
	service, err := NewAlarmService(sharedtestutil.NewTestCacheService(t.Context(), t), nil, repository, logger, []int{5})
	require.NoError(t, err)

	return service, pool
}

func TestAddAlarmAfterSubscriberEvictionPreservesEveryRecipient(t *testing.T) {
	for _, kind := range domain.AllAlarmTypes {
		t.Run(string(kind), func(t *testing.T) {
			service, pool := newEvictionAlarmService(t)
			ctx := t.Context()

			for _, room := range []string{testExistingRoomA, testExistingRoomB} {
				_, err := service.AddAlarm(ctx, &domain.AddAlarmRequest{RoomID: room, ChannelID: testChannelID, AlarmTypes: domain.AlarmTypes{kind}})
				require.NoError(t, err)
			}

			require.NoError(t, service.cache.Del(ctx, sharedalarmkeys.BuildChannelSubscriberKey(testChannelID, kind)))
			// 빈 구독 표식도 새 구독 뒤에는 정본 조회를 가리면 안 된다.
			require.NoError(t, service.cache.Set(ctx, sharedalarmkeys.BuildChannelSubscriberEmptyKey(testChannelID, kind), "1", time.Minute))

			added, err := service.AddAlarm(ctx, &domain.AddAlarmRequest{RoomID: testNewRoomC, ChannelID: testChannelID, AlarmTypes: domain.AlarmTypes{kind}})
			require.NoError(t, err)
			require.True(t, added)

			rooms, err := sharedalarm.NewSubscriberResolver(service.cache, pool).ResolveEventSubscribers(ctx, testChannelID, "", kind)
			require.NoError(t, err)
			require.ElementsMatch(t, []string{testExistingRoomA, testExistingRoomB, testNewRoomC}, rooms)
		})
	}
}

func TestRemoveAlarmAfterSubscriberEvictionKeepsSubscribedChannelDiscoverable(t *testing.T) {
	service, pool := newEvictionAlarmService(t)
	ctx := t.Context()

	for _, room := range []string{"leaving", "remaining"} {
		_, err := service.AddAlarm(ctx, &domain.AddAlarmRequest{RoomID: room, ChannelID: testChannelID, AlarmTypes: domain.AlarmTypes{domain.AlarmTypeLive}})
		require.NoError(t, err)
	}

	_, err := service.AddAlarm(ctx, &domain.AddAlarmRequest{RoomID: "other", ChannelID: testOtherChannelID})
	require.NoError(t, err)
	require.NoError(t, service.cache.Del(ctx, sharedalarmkeys.BuildChannelSubscriberKey(testChannelID, domain.AlarmTypeLive)))

	removed, err := service.RemoveAlarm(ctx, "leaving", testChannelID, nil)
	require.NoError(t, err)
	require.True(t, removed)

	channels, err := service.cache.SMembers(ctx, sharedalarmkeys.AlarmChannelRegistryKey)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{testChannelID, testOtherChannelID}, channels)

	rooms, err := sharedalarm.NewSubscriberResolver(service.cache, pool).ResolveEventSubscribers(ctx, testChannelID, "", domain.AlarmTypeLive)
	require.NoError(t, err)
	require.Equal(t, []string{"remaining"}, rooms)

	removed, err = service.RemoveAlarm(ctx, "remaining", testChannelID, nil)
	require.NoError(t, err)
	require.True(t, removed)

	channels, err = service.cache.SMembers(ctx, sharedalarmkeys.AlarmChannelRegistryKey)
	require.NoError(t, err)
	require.Equal(t, []string{testOtherChannelID}, channels)
}

func TestAddAlarmAfterRegistryEvictionRestoresAllSubscribedChannels(t *testing.T) {
	service, _ := newEvictionAlarmService(t)
	ctx := t.Context()

	for _, channel := range []string{testChannelID, testOtherChannelID} {
		_, err := service.AddAlarm(ctx, &domain.AddAlarmRequest{RoomID: testRoomID, ChannelID: channel})
		require.NoError(t, err)
	}

	require.NoError(t, service.cache.Del(ctx, sharedalarmkeys.AlarmChannelRegistryKey))

	added, err := service.AddAlarm(ctx, &domain.AddAlarmRequest{RoomID: testRoomID, ChannelID: "new-channel"})
	require.NoError(t, err)
	require.True(t, added)

	channels, err := service.cache.SMembers(ctx, sharedalarmkeys.AlarmChannelRegistryKey)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{testChannelID, testOtherChannelID, "new-channel"}, channels)
}

func TestAddAlarmCacheFailureKeepsCommittedRecipients(t *testing.T) {
	service, pool := newEvictionAlarmService(t)
	ctx := t.Context()
	_, err := service.AddAlarm(ctx, &domain.AddAlarmRequest{RoomID: "existing", ChannelID: testChannelID, AlarmTypes: domain.AlarmTypes{domain.AlarmTypeLive}})
	require.NoError(t, err)

	// commit 뒤 cache 갱신을 잘못된 자료형으로 실패시켜도, 먼저 commit한 구독은 수신 대상에 포함되어야 한다.
	require.NoError(t, service.cache.Set(ctx, sharedalarmkeys.MemberNameKey, "wrong-type", time.Minute))

	_, err = service.AddAlarm(ctx, &domain.AddAlarmRequest{RoomID: "committed", ChannelID: testChannelID, AlarmTypes: domain.AlarmTypes{domain.AlarmTypeLive}})
	require.Error(t, err)

	rooms, err := sharedalarm.NewSubscriberResolver(service.cache, pool).ResolveEventSubscribers(ctx, testChannelID, "", domain.AlarmTypeLive)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"existing", "committed"}, rooms)
}
