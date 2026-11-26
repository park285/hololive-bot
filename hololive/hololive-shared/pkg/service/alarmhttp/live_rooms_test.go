package alarmhttp

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	contract "github.com/kapu/hololive-shared/pkg/contracts/alarm"
)

func TestLiveSubscriberRoomsReadsTheRoomList(t *testing.T) {
	for _, tt := range []struct {
		body  string
		rooms []string
	}{
		{`{"success":true,"data":{"room_ids":["1","2"]}}`, []string{"1", "2"}},
		{`{"success":true,"data":{"room_ids":[]}}`, []string{}},
		{`{"success":true,"data":{}}`, nil},
		{`{"success":true,"data":{"room_ids":null}}`, nil},
	} {
		var path, key string

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			path, key = r.URL.Path, r.Header.Get("X-API-Key")
			w.Header().Set("Content-Type", "application/json")

			if _, err := w.Write([]byte(tt.body)); err != nil {
				t.Error(err)
			}
		}))
		t.Cleanup(server.Close)

		rooms, err := newTestClient(server.URL, "secret").LiveSubscriberRooms(t.Context(), " UC1 ")
		require.Equal(t, contract.LiveRoomsPath("UC1"), path)
		require.Equal(t, "secret", key)

		if tt.rooms == nil {
			require.Error(t, err)

			continue
		}

		require.NoError(t, err)
		require.Equal(t, tt.rooms, rooms)
	}

	_, err := newTestClient("http://127.0.0.1:1", "").LiveSubscriberRooms(t.Context(), " ")
	require.Error(t, err)
}
