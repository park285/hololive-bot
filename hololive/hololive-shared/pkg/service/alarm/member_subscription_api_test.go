package alarm

import (
	"context"
	"log/slog"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func newMemberSubscriptionAPIClient(t *testing.T) (*Client, <-chan domain.AddAlarmRequest, <-chan domain.Alarm) {
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
		getRoomAlarmsWithTypesFn: func(_ context.Context, roomID string) ([]*domain.Alarm, error) {
			return []*domain.Alarm{
				{RoomID: roomID, ChannelID: testUnitBChannel, HostID: testMiraHostID, MemberName: "미라"},
				{RoomID: roomID, ChannelID: testUnitBChannel, HostID: testNeonHostID, MemberName: "네온"},
			}, nil
		},
	}
	server := httptest.NewServer(newTestHandler(t, mock))
	t.Cleanup(server.Close)

	client := NewClient(server.URL, slog.New(slog.DiscardHandler))

	return client, added, removed
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

	alarms, err := client.GetRoomAlarmsWithTypes(ctx, testRoomID)
	require.NoError(t, err)
	require.Len(t, alarms, 2)
	require.Equal(t, testMiraHostID, alarms[0].HostID)

	channels, err := client.GetRoomAlarms(ctx, testRoomID)
	require.NoError(t, err)
	require.Equal(t, []string{testUnitBChannel}, channels)

	for _, hostID := range []string{"unknown-member", " "} {
		_, invalidErr := client.AddAlarm(ctx, &domain.AddAlarmRequest{RoomID: testRoomID, ChannelID: testUnitBChannel, HostID: hostID})
		require.Error(t, invalidErr)
	}

	_, err = client.RemoveHostAlarm(ctx, testRoomID, testUnitBChannel, "", types)
	require.Error(t, err)
	require.Empty(t, added)
	require.Empty(t, removed)
}
