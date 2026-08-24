// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package alarm

import (
	"context"
	jsonv2 "encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
)

type nilResponseTransport struct{}

func (nilResponseTransport) RoundTrip(*http.Request) (*http.Response, error) {
	var missing *http.Response

	return missing, nil
}

func TestClientDoRequestNilResponse(t *testing.T) {
	client := NewClient("https://alarm.example", nil)

	client.httpClient.Transport = nilResponseTransport{}

	resp, err := client.doRequest(t.Context(), http.MethodGet, "/alarms", http.NoBody, false)
	if resp != nil && resp.Body != nil {
		t.Cleanup(func() {
			if closeErr := resp.Body.Close(); closeErr != nil {
				t.Errorf("close response body: %v", closeErr)
			}
		})
	}

	require.Error(t, err)
	assert.Contains(t, err.Error(), "nil response")
}

func TestClientRejectsNilAddRequest(t *testing.T) {
	client := NewClient("https://alarm.example", nil)
	added, err := client.AddAlarm(t.Context(), nil)
	require.Error(t, err)
	assert.False(t, added)
}

func newRoundTripAlarmMock(t *testing.T) *mockAlarmCRUD {
	t.Helper()

	now := time.Now().UTC().Truncate(time.Second)
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

		return []*domain.Alarm{{RoomID: roomID, ChannelID: testClientChannelID, MemberName: "Miko"}}, nil
	}
	mock.listRoomAlarmsViewFn = func(_ context.Context, _ string) ([]domain.AlarmListView, error) {
		return []domain.AlarmListView{{
			ChannelID:  testClientChannelID,
			MemberName: "Miko",
			AlarmTypes: domain.AlarmTypes{domain.AlarmTypeLive},
			NextStream: &domain.NextStreamInfo{Status: domain.NextStreamStatusUpcoming, VideoID: "video1", StartScheduled: &now},
		}}, nil
	}
	mock.clearRoomAlarmsFn = func(_ context.Context, roomID string) (int, error) {
		assert.Equal(t, testRoomID, roomID)

		return 3, nil
	}
	mock.getNextStreamInfoFn = func(_ context.Context, channelID string) (*domain.NextStreamInfo, error) {
		assert.Equal(t, testClientChannelID, channelID)

		return &domain.NextStreamInfo{Status: domain.NextStreamStatusUpcoming, VideoID: "video1", StartScheduled: &now}, nil
	}
	mock.updateAlarmAdvanceMinutesFn = func(minutes int) []int {
		assert.Equal(t, 10, minutes)

		return []int{10, 5, 1}
	}
	mock.setRoomNameFn = func(_ context.Context, roomID, roomName string) error {
		assert.Equal(t, testRoomID, roomID)
		assert.Equal(t, "Room One", roomName)

		return nil
	}
	mock.setUserNameFn = func(_ context.Context, userID, userName string) error {
		assert.Equal(t, "user1", userID)
		assert.Equal(t, "User One", userName)

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

	client := NewClient(server.URL, nil)

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
	assert.Equal(t, "video1", views[0].NextStream.VideoID)

	deleted, err := client.ClearRoomAlarms(t.Context(), testRoomID)
	require.NoError(t, err)
	assert.Equal(t, 3, deleted)

	next, err := client.GetNextStreamInfo(t.Context(), testClientChannelID)
	require.NoError(t, err)
	require.NotNil(t, next)
	assert.Equal(t, "video1", next.VideoID)

	assert.Equal(t, []int{10, 5, 1}, client.UpdateAlarmAdvanceMinutes(t.Context(), 10))
	assert.Equal(t, []int{10, 5, 1}, client.GetTargetMinutes())

	require.NoError(t, client.SetRoomName(t.Context(), testRoomID, "Room One"))
	require.NoError(t, client.SetUserName(t.Context(), "user1", "User One"))

	keys, err := client.GetAllAlarmKeys(t.Context())
	require.NoError(t, err)
	require.Len(t, keys, 1)
	assert.Equal(t, testClientChannelID, keys[0].ChannelID)
}

func TestClientHandlesNullProviderData(t *testing.T) {
	mock := completeAlarmMock()

	mock.getNextStreamInfoFn = func(context.Context, string) (*domain.NextStreamInfo, error) {
		var missing *domain.NextStreamInfo

		return missing, nil
	}

	server := httptest.NewServer(newTestHandler(t, mock))
	t.Cleanup(server.Close)

	client := NewClient(server.URL, nil)

	next, err := client.GetNextStreamInfo(t.Context(), testClientChannelID)
	require.NoError(t, err)
	assert.Nil(t, next)
}

func TestClientRejectsUnsuccessfulEnvelope(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		assert.NoError(t, jsonv2.MarshalWrite(w, APIResponse{Success: false, Error: "alarm_add_failed", Message: "add failed"}))
	}))
	t.Cleanup(server.Close)

	client := NewClient(server.URL, nil)
	_, err := client.AddAlarm(t.Context(), &domain.AddAlarmRequest{RoomID: testRoomID, ChannelID: testClientChannelID})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "add failed")
}

func TestClientSendsAPIKeyHeader(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "secret-key", r.Header.Get("X-API-Key"))
		w.Header().Set("Content-Type", "application/json")
		assert.NoError(t, jsonv2.MarshalWrite(w, APIResponse{Success: true, Data: map[string]bool{"added": true}}))
	}))
	t.Cleanup(server.Close)

	client := NewClientWithAPIKey(server.URL, "secret-key", nil)
	added, err := client.AddAlarm(t.Context(), &domain.AddAlarmRequest{RoomID: testRoomID, ChannelID: testClientChannelID})
	require.NoError(t, err)
	assert.True(t, added)
}

func TestDecodeAPIEnvelopeRejectsMalformedAndMissingSuccess(t *testing.T) {
	for _, body := range []string{"not-json", `{"data":{"added":true}}`} {
		got, err := decodeAPIEnvelope[addAlarmResp]("/test", strings.NewReader(body))
		require.Error(t, err)
		assert.Equal(t, addAlarmResp{}, got)
	}
}

func TestClientPutNoDataIgnoresDataAndSurfacesEnvelopeFailure(t *testing.T) {
	t.Parallel()

	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		assert.NoError(t, jsonv2.MarshalWrite(w, APIResponse{Success: true, Data: map[string]string{"ignored": "value"}}))
	}))
	t.Cleanup(ok.Close)
	require.NoError(t, NewClient(ok.URL, nil).SetRoomName(t.Context(), testRoomID, "Room One"))

	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		assert.NoError(t, jsonv2.MarshalWrite(w, APIResponse{Success: false, Message: "rename failed"}))
	}))
	t.Cleanup(failing.Close)

	err := NewClient(failing.URL, nil).SetUserName(t.Context(), "user1", "User One")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "rename failed")
}

func completeAlarmMock() *mockAlarmCRUD {
	return &mockAlarmCRUD{
		addAlarmFn:                  func(context.Context, domain.AddAlarmRequest) (bool, error) { return false, nil },
		removeAlarmFn:               func(context.Context, string, string, domain.AlarmTypes) (bool, error) { return false, nil },
		getRoomAlarmsFn:             func(context.Context, string) ([]string, error) { return nil, nil },
		getRoomAlarmsWithTypesFn:    func(context.Context, string) ([]*domain.Alarm, error) { return nil, nil },
		listRoomAlarmsViewFn:        func(context.Context, string) ([]domain.AlarmListView, error) { return nil, nil },
		clearRoomAlarmsFn:           func(context.Context, string) (int, error) { return 0, nil },
		getNextStreamInfoFn:         func(context.Context, string) (*domain.NextStreamInfo, error) { return nil, nil },
		updateAlarmAdvanceMinutesFn: func(int) []int { return nil },
		getTargetMinutesFn:          func() []int { return nil },
		setRoomNameFn:               func(context.Context, string, string) error { return nil },
		setUserNameFn:               func(context.Context, string, string) error { return nil },
		getAllAlarmKeysFn:           func(context.Context) ([]*domain.AlarmEntry, error) { return nil, nil },
		warmCacheFromDBFn:           func(context.Context) error { return nil },
	}
}
