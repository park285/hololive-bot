package alarm

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
)

const (
	testUnitBChannel     = "UC3OH5FKQ3qtl4uRme_vZTgA"
	testMiraHostID       = "reimei-mira"
	testNeonHostID       = "yoinagi-neon"
	testUnitBWholeRoom   = "whole"
	testUnitBMiraRoom    = "mira"
	testUnitBNeonRoom    = "neon"
	testUnitBSeveralRoom = "several"
)

func TestMemberSubscriptionsPersistIndependently(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	repo := &Repository{pool: pool}

	for _, hostID := range []string{"", "kiyosumi-lyra", testMiraHostID, testNeonHostID} {
		require.NoError(t, repo.Add(ctx, &domain.Alarm{
			RoomID: testRoomID, ChannelID: testUnitBChannel, HostID: hostID,
			AlarmTypes: domain.AlarmTypes{domain.AlarmTypeLive},
		}))
	}

	alarms, err := repo.FindByRoom(ctx, testRoomID)
	require.NoError(t, err)
	require.Len(t, alarms, 4)

	require.NoError(t, repo.Add(ctx, &domain.Alarm{
		RoomID: testRoomID, ChannelID: testUnitBChannel, HostID: testMiraHostID,
		AlarmTypes: domain.AlarmTypes{domain.AlarmTypeShorts},
	}))

	alarms, err = repo.FindByRoom(ctx, testRoomID)
	require.NoError(t, err)
	require.Len(t, alarms, 4)

	for _, alarm := range alarms {
		want := domain.AlarmTypes{domain.AlarmTypeLive}

		if alarm.HostID == testMiraHostID {
			want = domain.AlarmTypes{domain.AlarmTypeShorts}
		}

		require.Equal(t, want, alarm.AlarmTypes)
	}

	require.Error(t, repo.RemoveHost(ctx, testRoomID, testUnitBChannel, ""))
	require.NoError(t, repo.RemoveHost(ctx, testRoomID, testUnitBChannel, testMiraHostID))
	require.NoError(t, repo.Remove(ctx, testRoomID, testUnitBChannel))

	alarms, err = repo.FindByRoom(ctx, testRoomID)
	require.NoError(t, err)
	require.Len(t, alarms, 2)

	for _, alarm := range alarms {
		require.Contains(t, []string{"kiyosumi-lyra", testNeonHostID}, alarm.HostID)
	}

	deleted, err := repo.ClearByRoom(ctx, testRoomID)
	require.NoError(t, err)
	require.EqualValues(t, 2, deleted)

	for _, invalid := range []*domain.Alarm{
		{RoomID: testRoomID, ChannelID: testUnitBChannel, HostID: "unknown-member"},
		{RoomID: testRoomID, ChannelID: "other-channel", HostID: testMiraHostID},
	} {
		require.Error(t, repo.Add(ctx, invalid))
	}
}

func TestResolveEventSubscribersUsesMemberUnionAndFailOpen(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	repo := &Repository{pool: pool}

	for _, alarm := range []*domain.Alarm{
		{RoomID: testUnitBWholeRoom, ChannelID: testUnitBChannel},
		{RoomID: testUnitBMiraRoom, ChannelID: testUnitBChannel, HostID: testMiraHostID},
		{RoomID: "lyra", ChannelID: testUnitBChannel, HostID: "kiyosumi-lyra", AlarmTypes: domain.AlarmTypes{domain.AlarmTypeLive}},
		{RoomID: testUnitBNeonRoom, ChannelID: testUnitBChannel, HostID: testNeonHostID},
		{RoomID: testUnitBSeveralRoom, ChannelID: testUnitBChannel, HostID: testMiraHostID},
		{RoomID: testUnitBSeveralRoom, ChannelID: testUnitBChannel, HostID: testNeonHostID},
		{RoomID: "community-only", ChannelID: testUnitBChannel, HostID: testMiraHostID, AlarmTypes: domain.AlarmTypes{domain.AlarmTypeCommunity}},
		{RoomID: "unrelated", ChannelID: "other-channel"},
	} {
		require.NoError(t, repo.Add(ctx, alarm))
	}

	for _, tc := range []struct {
		name, title string
		alarmType   domain.AlarmType
		rooms       []string
	}{
		{testUnitBMiraRoom, "#玲銘ミラ", domain.AlarmTypeLive, []string{testUnitBWholeRoom, testUnitBMiraRoom, testUnitBSeveralRoom}},
		{testUnitBNeonRoom, "#宵凪ネオン", domain.AlarmTypeLive, []string{testUnitBWholeRoom, testUnitBNeonRoom, testUnitBSeveralRoom}},
		{"cohosts", "#玲銘ミラ #宵凪ネオン", domain.AlarmTypeLive, []string{testUnitBWholeRoom, testUnitBMiraRoom, testUnitBNeonRoom, testUnitBSeveralRoom}},
		{"unknown", "今日は何をしよう？ #UNIT_B", domain.AlarmTypeLive, []string{testUnitBWholeRoom, testUnitBMiraRoom, "lyra", testUnitBNeonRoom, testUnitBSeveralRoom}},
		{"empty", "", domain.AlarmTypeLive, []string{testUnitBWholeRoom, testUnitBMiraRoom, "lyra", testUnitBNeonRoom, testUnitBSeveralRoom}},
		{"guest_only", "#墨汐さやな", domain.AlarmTypeLive, []string{testUnitBWholeRoom, testUnitBMiraRoom, "lyra", testUnitBNeonRoom, testUnitBSeveralRoom}},
		{"foreign_guest", "りらら×ミラ", domain.AlarmTypeLive, []string{testUnitBWholeRoom, testUnitBMiraRoom, testUnitBSeveralRoom}},
		{"shorts", "#清澄ライラ", domain.AlarmTypeShorts, []string{testUnitBWholeRoom}},
		{"unknown_shorts", "?", domain.AlarmTypeShorts, []string{testUnitBWholeRoom, testUnitBMiraRoom, testUnitBNeonRoom, testUnitBSeveralRoom}},
		{"community", "", domain.AlarmTypeCommunity, []string{testUnitBWholeRoom, testUnitBMiraRoom, testUnitBNeonRoom, testUnitBSeveralRoom, "community-only"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rooms, err := ResolveEventSubscribers(ctx, nil, pool, testUnitBChannel, tc.title, tc.alarmType)
			require.NoError(t, err)
			require.ElementsMatch(t, tc.rooms, rooms)
		})
	}

	rooms, err := ResolveEventSubscribers(ctx, nil, pool, "other-channel", "#玲銘ミラ", domain.AlarmTypeLive)
	require.NoError(t, err)
	require.Equal(t, []string{"unrelated"}, rooms)

	_, err = ResolveEventSubscribers(ctx, nil, nil, testUnitBChannel, "?", domain.AlarmTypeLive)
	require.ErrorContains(t, err, "database is nil")

	canceled, cancel := context.WithCancel(ctx)
	cancel()

	_, err = ResolveEventSubscribers(canceled, nil, pool, testUnitBChannel, "?", domain.AlarmTypeLive)
	require.ErrorIs(t, err, context.Canceled)
}
