package alarmservice

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
	sharedalarm "github.com/kapu/hololive-shared/pkg/service/alarm"
	sharedalarmkeys "github.com/kapu/hololive-shared/pkg/service/alarm/keys"
	databasemocks "github.com/kapu/hololive-shared/pkg/service/database/mocks"
)

const memberSubscriptionChannel = "UC3OH5FKQ3qtl4uRme_vZTgA"

const memberSubscriptionMiraID = "reimei-mira"

func newMemberSubscriptionService(t *testing.T) *AlarmService {
	t.Helper()

	as := newTestAlarmService(t)
	pool := dbtest.NewPool(t)
	repo := sharedalarm.NewRepository(&databasemocks.Client{GetPoolFunc: func() *pgxpool.Pool { return pool }}, as.logger)

	as.alarmRepository, as.alarmWriter = repo, repo
	as.memberData = &mockMemberDataProvider{members: []*domain.Member{{ChannelID: memberSubscriptionChannel, Name: "유닛 B"}}}

	return as
}

func seedMemberSubscriptionChoices(t *testing.T, as *AlarmService, choices map[string]domain.AlarmTypes) {
	t.Helper()

	for hostID, types := range choices {
		added, err := as.AddAlarm(t.Context(), &domain.AddAlarmRequest{
			RoomID: testRoomID, ChannelID: memberSubscriptionChannel, HostID: hostID,
			MemberName: "사용자가 보낸 표시명", AlarmTypes: types,
		})
		require.NoError(t, err)
		require.True(t, added, "new member choice must be reported even when the channel is already cached")
	}
}

func requireMemberSubscriptionCache(t *testing.T, as *AlarmService, wantTypes domain.AlarmTypes) {
	t.Helper()

	for _, alarmType := range domain.AllAlarmTypes {
		rooms, err := as.GetChannelSubscribersByType(t.Context(), memberSubscriptionChannel, alarmType)
		require.NoError(t, err)

		if wantTypes.Contains(alarmType) {
			require.Equal(t, []string{testRoomID}, rooms)
		} else {
			require.Empty(t, rooms)
		}
	}

	registered, err := as.cache.SIsMember(t.Context(), sharedalarmkeys.AlarmChannelRegistryKey, memberSubscriptionChannel)
	require.NoError(t, err)
	require.Equal(t, len(wantTypes) > 0, registered)
}

func TestMemberSubscriptionsKeepOtherRoomChoicesAndCache(t *testing.T) {
	ctx := t.Context()
	as := newMemberSubscriptionService(t)
	seedMemberSubscriptionChoices(t, as, map[string]domain.AlarmTypes{
		memberSubscriptionMiraID: {domain.AlarmTypeLive, domain.AlarmTypeShorts},
		"yoinagi-neon":           {domain.AlarmTypeLive},
		"kiyosumi-lyra":          {domain.AlarmTypeCommunity},
		"":                       {domain.AlarmTypeLive},
	})

	channelName, err := as.GetMemberName(ctx, memberSubscriptionChannel)
	require.NoError(t, err)
	require.Equal(t, "유닛 B", channelName)

	removed, err := as.RemoveHostAlarm(ctx, testRoomID, memberSubscriptionChannel, memberSubscriptionMiraID, domain.AlarmTypes{domain.AlarmTypeLive})
	require.NoError(t, err)
	require.True(t, removed)

	removed, err = as.RemoveAlarm(ctx, testRoomID, memberSubscriptionChannel, nil)
	require.NoError(t, err)
	require.True(t, removed)

	liveRooms, err := as.GetChannelSubscribersByType(ctx, memberSubscriptionChannel, domain.AlarmTypeLive)
	require.NoError(t, err)
	require.Equal(t, []string{testRoomID}, liveRooms)

	removed, err = as.RemoveHostAlarm(ctx, testRoomID, memberSubscriptionChannel, "yoinagi-neon", nil)
	require.NoError(t, err)
	require.True(t, removed)

	wantCachedTypes := domain.AlarmTypes{domain.AlarmTypeShorts, domain.AlarmTypeCommunity}
	requireMemberSubscriptionCache(t, as, wantCachedTypes)

	require.NoError(t, as.WarmCacheFromDB(ctx))
	requireMemberSubscriptionCache(t, as, wantCachedTypes)

	views, err := as.ListRoomAlarmsView(ctx, testRoomID)
	require.NoError(t, err)
	require.Len(t, views, 2)

	want := map[string]domain.AlarmTypes{
		"미라[유닛b]": {domain.AlarmTypeShorts}, "라이라[유닛b]": {domain.AlarmTypeCommunity},
	}

	for _, view := range views {
		require.Equal(t, want[view.MemberName], view.AlarmTypes)
		require.NotEmpty(t, view.HostID)
	}

	added, err := as.AddAlarm(ctx, &domain.AddAlarmRequest{
		RoomID: testRoomID, ChannelID: memberSubscriptionChannel, HostID: memberSubscriptionMiraID,
		AlarmTypes: domain.AlarmTypes{domain.AlarmTypeShorts},
	})
	require.NoError(t, err)
	require.False(t, added)

	count, err := as.ClearRoomAlarms(ctx, testRoomID)
	require.NoError(t, err)
	require.Equal(t, 2, count)

	alarms, err := as.GetRoomAlarmsWithTypes(ctx, testRoomID)
	require.NoError(t, err)
	require.Empty(t, alarms)

	rooms, err := as.GetRoomAlarms(ctx, testRoomID)
	require.NoError(t, err)
	require.Empty(t, rooms)
}

func TestMemberSubscriptionsRejectUnavailableOrInvalidTargets(t *testing.T) {
	as := newTestAlarmService(t)

	for _, hostID := range []string{memberSubscriptionMiraID, "not-a-member", " "} {
		added, err := as.AddAlarm(t.Context(), &domain.AddAlarmRequest{
			RoomID: testRoomID, ChannelID: memberSubscriptionChannel, HostID: hostID,
		})
		require.Error(t, err)
		require.False(t, added)
	}

	removed, err := as.RemoveHostAlarm(t.Context(), testRoomID, memberSubscriptionChannel, "", nil)
	require.Error(t, err)
	require.False(t, removed)

	rooms, err := as.GetRoomAlarms(t.Context(), testRoomID)
	require.NoError(t, err)
	require.Empty(t, rooms)
}

func TestMemberSubscriptionRemovalRebuildsCacheAfterRefreshFailure(t *testing.T) {
	ctx := t.Context()
	as := newMemberSubscriptionService(t)
	seedMemberSubscriptionChoices(t, as, map[string]domain.AlarmTypes{
		memberSubscriptionMiraID: {domain.AlarmTypeShorts},
		"yoinagi-neon":           {domain.AlarmTypeLive},
	})

	original := findRoomAlarmsFromRepository

	t.Cleanup(func() { findRoomAlarmsFromRepository = original })

	lookupErr := errors.New("remaining subscriptions lookup failed")
	calls := 0

	findRoomAlarmsFromRepository = func(ctx context.Context, repository *sharedalarm.Repository, roomID string) ([]*domain.Alarm, error) {
		calls++
		if calls == 2 {
			return nil, lookupErr
		}

		return original(ctx, repository, roomID)
	}

	removed, err := as.RemoveHostAlarm(ctx, testRoomID, memberSubscriptionChannel, memberSubscriptionMiraID, nil)
	require.ErrorIs(t, err, lookupErr)
	require.False(t, removed)

	alarms, err := as.GetRoomAlarmsWithTypes(ctx, testRoomID)
	require.NoError(t, err)
	require.Len(t, alarms, 1)
	require.Equal(t, "yoinagi-neon", alarms[0].HostID)

	shortsRooms, err := as.GetChannelSubscribersByType(ctx, memberSubscriptionChannel, domain.AlarmTypeShorts)
	require.NoError(t, err)
	require.Empty(t, shortsRooms)

	liveRooms, err := as.GetChannelSubscribersByType(ctx, memberSubscriptionChannel, domain.AlarmTypeLive)
	require.NoError(t, err)
	require.Equal(t, []string{testRoomID}, liveRooms)
}

func TestMemberSubscriptionViewDoesNotUseAnotherMembersNextStream(t *testing.T) {
	alarms := []*domain.Alarm{
		{ChannelID: memberSubscriptionChannel, HostID: memberSubscriptionMiraID, AlarmTypes: domain.AlarmTypes{domain.AlarmTypeLive}},
		{ChannelID: memberSubscriptionChannel, HostID: "yoinagi-neon", AlarmTypes: domain.AlarmTypes{domain.AlarmTypeLive}},
		{ChannelID: memberSubscriptionChannel, HostID: "yoinagi-neon", AlarmTypes: domain.AlarmTypes{domain.AlarmTypeShorts}},
		{ChannelID: memberSubscriptionChannel, AlarmTypes: domain.AlarmTypes{domain.AlarmTypeLive}},
		{ChannelID: "other-channel", MemberName: "페코라", AlarmTypes: domain.AlarmTypes{domain.AlarmTypeLive}},
	}
	next := &domain.NextStreamInfo{Status: domain.NextStreamStatusUpcoming, Title: "#宵凪ネオン"}
	views := buildAlarmListViews(alarms, map[string]string{memberSubscriptionChannel: "유닛 B"}, map[string]*domain.NextStreamInfo{memberSubscriptionChannel: next})
	require.Equal(t, "미라[유닛b]", views[0].MemberName)
	require.Nil(t, views[0].NextStream)
	require.Equal(t, "네온[유닛b]", views[1].MemberName)
	require.Same(t, next, views[1].NextStream)
	require.Nil(t, views[2].NextStream)
	require.Equal(t, "유닛 B", views[3].MemberName)
	require.Equal(t, "페코라", views[4].MemberName)
}
