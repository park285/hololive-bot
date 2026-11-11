package subscriptions

import (
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/alarmstore"
	alarmkeys "github.com/kapu/hololive-alarm-worker/internal/service/alarm/keys"
	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
	databasemocks "github.com/kapu/hololive-shared/pkg/service/database/mocks"
	sharedtestutil "github.com/kapu/hololive-shared/pkg/testutil"
)

func newEvictionAlarmService(t *testing.T) (*AlarmService, *pgxpool.Pool) {
	t.Helper()

	pool := dbtest.NewPool(t)
	logger := newDiscardAlarmLogger()
	repository := alarmstore.NewRepository(&databasemocks.Client{GetPoolFunc: func() *pgxpool.Pool { return pool }}, logger)
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

			require.NoError(t, service.cache.Del(ctx, alarmkeys.BuildChannelSubscriberKey(testChannelID, kind)))

			added, err := service.AddAlarm(ctx, &domain.AddAlarmRequest{RoomID: testNewRoomC, ChannelID: testChannelID, AlarmTypes: domain.AlarmTypes{kind}})
			require.NoError(t, err)
			require.True(t, added)

			rooms, err := alarmstore.NewSubscriberResolver(service.cache, pool).ResolveEventSubscribers(ctx, testChannelID, "", kind)
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
	require.NoError(t, service.cache.Del(ctx, alarmkeys.BuildChannelSubscriberKey(testChannelID, domain.AlarmTypeLive)))

	removed, err := service.RemoveAlarm(ctx, "leaving", testChannelID, nil)
	require.NoError(t, err)
	require.True(t, removed)

	channels, err := service.cache.SMembers(ctx, alarmkeys.AlarmChannelRegistryKey)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{testChannelID, testOtherChannelID}, channels)

	rooms, err := alarmstore.NewSubscriberResolver(service.cache, pool).ResolveEventSubscribers(ctx, testChannelID, "", domain.AlarmTypeLive)
	require.NoError(t, err)
	require.Equal(t, []string{"remaining"}, rooms)

	removed, err = service.RemoveAlarm(ctx, "remaining", testChannelID, nil)
	require.NoError(t, err)
	require.True(t, removed)

	channels, err = service.cache.SMembers(ctx, alarmkeys.AlarmChannelRegistryKey)
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

	require.NoError(t, service.cache.Del(ctx, alarmkeys.AlarmChannelRegistryKey))

	added, err := service.AddAlarm(ctx, &domain.AddAlarmRequest{RoomID: testRoomID, ChannelID: "new-channel"})
	require.NoError(t, err)
	require.True(t, added)

	channels, err := service.cache.SMembers(ctx, alarmkeys.AlarmChannelRegistryKey)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{testChannelID, testOtherChannelID, "new-channel"}, channels)
}
