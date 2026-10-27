package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http/httptest"
	"testing"

	sharedh3 "github.com/park285/shared-go/v2/pkg/h3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/alarmhttp"
)

// newRoundTripClient는 운영 생성자로 httptest handler를 가리키는 alarmhttp client를 만든다.
// 서버가 worker로 옮겨왔으므로 실제 handler와 client의 envelope 왕복은 이 패키지가 검증한다.
func newRoundTripClient(t *testing.T, baseURL string) *alarmhttp.Client {
	t.Helper()

	client, err := alarmhttp.NewClientWithAPIKeyStrict(baseURL, "", slog.New(slog.DiscardHandler), sharedh3.ClientOptions{})
	require.NoError(t, err)
	t.Cleanup(func() {
		if closeErr := client.Close(); closeErr != nil {
			t.Errorf("close alarm client: %v", closeErr)
		}
	})

	return client
}

func newRoundTripAlarmMock(t *testing.T) *mockAlarmCRUD {
	t.Helper()

	mock := completeAlarmMock()

	mock.addAlarmFn = func(_ context.Context, req domain.AddAlarmRequest) (bool, error) {
		assert.Equal(t, testRoomID, req.RoomID)
		assert.Equal(t, testClientChannelID, req.ChannelID)

		return true, nil
	}
	mock.removeAlarmFn = func(_ context.Context, roomID, channelID string, _ domain.AlarmTypes) (bool, error) {
		assert.Equal(t, testRoomID, roomID)
		assert.Equal(t, testClientChannelID, channelID)

		return true, nil
	}
	mock.getRoomAlarmsWithTypesFn = func(_ context.Context, roomID string) ([]*domain.Alarm, error) {
		assert.Equal(t, testRoomID, roomID)

		return []*domain.Alarm{{RoomID: roomID, ChannelID: testClientChannelID}}, nil
	}
	mock.listRoomAlarmsViewFn = func(_ context.Context, _ string) ([]domain.AlarmListView, error) {
		return []domain.AlarmListView{{
			ChannelID:  testClientChannelID,
			MemberName: "Miko",
			AlarmTypes: domain.AlarmTypes{domain.AlarmTypeLive},
		}}, nil
	}
	mock.clearRoomAlarmsFn = func(_ context.Context, roomID string) (int, error) {
		assert.Equal(t, testRoomID, roomID)

		return 3, nil
	}
	mock.updateAlarmAdvanceMinutesFn = func(minutes int) (domain.AdvanceMinutesResult, error) {
		assert.Equal(t, 10, minutes)

		return domain.AdvanceMinutesResult{RequestedMinutes: minutes, Outcome: domain.ApplyConfirmed, TargetMinutes: []int{10, 5, 1}}, nil
	}
	mock.setRoomNameFn = func(_ context.Context, roomID, roomName string) error {
		assert.Equal(t, testRoomID, roomID)
		assert.Equal(t, "Room One", roomName)

		return nil
	}
	mock.getAllAlarmKeysFn = func(context.Context) ([]*domain.AlarmEntry, error) {
		return []*domain.AlarmEntry{{RoomID: testRoomID, ChannelID: testClientChannelID}}, nil
	}

	return mock
}

func TestClientRoundTripWithRealHandlerEnvelope(t *testing.T) {
	server := httptest.NewServer(newTestHandler(t, newRoundTripAlarmMock(t)))
	t.Cleanup(server.Close)

	client := newRoundTripClient(t, server.URL)

	added, err := client.AddAlarm(t.Context(), &domain.AddAlarmRequest{RoomID: testRoomID, ChannelID: testClientChannelID})
	require.NoError(t, err)
	assert.True(t, added)

	removed, err := client.RemoveAlarm(t.Context(), testRoomID, testClientChannelID, domain.AllAlarmTypes)
	require.NoError(t, err)
	assert.True(t, removed)

	alarms, err := client.GetRoomAlarmsWithTypes(t.Context(), testRoomID)
	require.NoError(t, err)
	require.Len(t, alarms, 1)
	assert.Equal(t, testClientChannelID, alarms[0].ChannelID)

	channelIDs, err := client.GetRoomAlarms(t.Context(), testRoomID)
	require.NoError(t, err)
	assert.Equal(t, []string{testClientChannelID}, channelIDs)

	views, err := client.ListRoomAlarmsView(t.Context(), testRoomID)
	require.NoError(t, err)
	require.Len(t, views, 1)
	assert.Equal(t, domain.AlarmTypes{domain.AlarmTypeLive}, views[0].AlarmTypes)

	deleted, err := client.ClearRoomAlarms(t.Context(), testRoomID)
	require.NoError(t, err)
	assert.Equal(t, 3, deleted)

	advanceResult, err := client.UpdateAlarmAdvanceMinutes(t.Context(), 10)
	require.NoError(t, err)
	assert.Equal(t, domain.ApplyConfirmed, advanceResult.Outcome)
	assert.Equal(t, []int{10, 5, 1}, advanceResult.TargetMinutes)
	assert.Equal(t, []int{10, 5, 1}, client.GetTargetMinutes())

	require.NoError(t, client.SetRoomName(t.Context(), testRoomID, "Room One"))

	keys, err := client.GetAllAlarmKeys(t.Context())
	require.NoError(t, err)
	require.Len(t, keys, 1)
	assert.Equal(t, testClientChannelID, keys[0].ChannelID)
}

func TestCountHTTPRoundTripDoesNotLoadAlarmList(t *testing.T) {
	for _, fail := range []bool{false, true} {
		mock := &mockAlarmCRUD{countAlarmEntriesFn: func(context.Context) (int, error) {
			if fail {
				return 0, errors.New("database unavailable")
			}

			return 3, nil
		}}
		server := httptest.NewServer(newTestHandler(t, mock))
		t.Cleanup(server.Close)

		count, err := newRoundTripClient(t, server.URL).CountAlarmEntries(t.Context())

		if fail {
			require.Error(t, err)
		} else {
			require.NoError(t, err)
			require.Equal(t, 3, count)
		}
	}
}

func completeAlarmMock() *mockAlarmCRUD {
	return &mockAlarmCRUD{
		addAlarmFn:                  func(context.Context, domain.AddAlarmRequest) (bool, error) { return false, nil },
		removeAlarmFn:               func(context.Context, string, string, domain.AlarmTypes) (bool, error) { return false, nil },
		getRoomAlarmsWithTypesFn:    func(context.Context, string) ([]*domain.Alarm, error) { return nil, nil },
		listRoomAlarmsViewFn:        func(context.Context, string) ([]domain.AlarmListView, error) { return nil, nil },
		clearRoomAlarmsFn:           func(context.Context, string) (int, error) { return 0, nil },
		updateAlarmAdvanceMinutesFn: func(int) (domain.AdvanceMinutesResult, error) { return domain.AdvanceMinutesResult{}, nil },
		setRoomNameFn:               func(context.Context, string, string) error { return nil },
		getAllAlarmKeysFn:           func(context.Context) ([]*domain.AlarmEntry, error) { return nil, nil },
	}
}
