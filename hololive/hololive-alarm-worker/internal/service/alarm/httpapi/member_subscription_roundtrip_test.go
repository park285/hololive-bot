package httpapi

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/alarmhttp"
)

func newMemberSubscriptionAPIClient(t *testing.T) (*alarmhttp.Client, <-chan domain.AddAlarmRequest, <-chan domain.Alarm) {
	t.Helper()

	added := make(chan domain.AddAlarmRequest, 4)
	removed := make(chan domain.Alarm, 4)
	mock := &mockAlarmCRUD{
		addAlarmFn: func(_ context.Context, req domain.AddAlarmRequest) (bool, error) {
			added <- req
			return true, nil
		},
		removeAlarmFn: func(_ context.Context, roomID, channelID string, types domain.AlarmTypes) (bool, error) {
			removed <- domain.Alarm{RoomID: roomID, ChannelID: channelID, AlarmTypes: types}
			return true, nil
		},
		removeHostAlarmFn: func(_ context.Context, roomID, channelID, hostID string, types domain.AlarmTypes) (bool, error) {
			removed <- domain.Alarm{RoomID: roomID, ChannelID: channelID, HostID: hostID, AlarmTypes: types}
			return true, nil
		},
		listRoomAlarmsViewFn: func(context.Context, string) ([]domain.AlarmListView, error) {
			return []domain.AlarmListView{
				{ChannelID: testUnitBChannel, HostID: testMiraHostID, AlarmTypes: domain.AlarmTypes{domain.AlarmTypeLive}},
				{ChannelID: testUnitBChannel, HostID: testNeonHostID, AlarmTypes: domain.AlarmTypes{domain.AlarmTypeLive}},
			}, nil
		},
	}
	server := httptest.NewServer(newTestHandler(t, mock))
	t.Cleanup(server.Close)

	return newRoundTripClient(t, server.URL), added, removed
}

func TestMemberSubscriptionClientAndAPIRoundTrip(t *testing.T) {
	client, added, removed := newMemberSubscriptionAPIClient(t)
	types := domain.AlarmTypes{domain.AlarmTypeLive}
	ctx := t.Context()

	ok, err := client.AddAlarm(ctx, &domain.AddAlarmRequest{
		RoomID: testRoomID, ChannelID: testUnitBChannel, HostID: testMiraHostID, AlarmTypes: types,
	})
	require.NoError(t, err)
	require.True(t, ok)
	require.Len(t, added, 1)

	request := <-added
	require.Equal(t, testMiraHostID, request.HostID)
	require.Equal(t, testRoomID, request.RoomID)

	ok, err = client.RemoveHostAlarm(ctx, testRoomID, testUnitBChannel, testMiraHostID, types)
	require.NoError(t, err)
	require.True(t, ok)

	ok, err = client.RemoveAlarm(ctx, testRoomID, testUnitBChannel, types)
	require.NoError(t, err)
	require.True(t, ok)
	require.Len(t, removed, 2)

	memberRemoval, channelRemoval := <-removed, <-removed
	require.Equal(t, testMiraHostID, memberRemoval.HostID)
	require.Empty(t, channelRemoval.HostID)
	require.Equal(t, testUnitBChannel, memberRemoval.ChannelID)

	views, err := client.ListRoomAlarmsView(ctx, testRoomID)
	require.NoError(t, err)
	require.Len(t, views, 2)
	require.Equal(t, []string{testMiraHostID, testNeonHostID}, []string{views[0].HostID, views[1].HostID})
	require.Equal(t, testUnitBChannel, views[0].ChannelID)

	for _, hostID := range []string{"unknown-member", " "} {
		_, invalidErr := client.AddAlarm(ctx, &domain.AddAlarmRequest{RoomID: testRoomID, ChannelID: testUnitBChannel, HostID: hostID})
		require.Error(t, invalidErr)
	}

	_, err = client.RemoveHostAlarm(ctx, testRoomID, testUnitBChannel, "", types)
	require.Error(t, err)
	require.Empty(t, added)
	require.Empty(t, removed)
}
