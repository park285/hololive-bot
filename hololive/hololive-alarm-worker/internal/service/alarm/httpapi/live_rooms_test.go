package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-alarm-worker/internal/service/eventgrade"
	contract "github.com/kapu/hololive-shared/pkg/contracts/alarm"
)

type fakeLiveRooms struct {
	rooms   []string
	err     error
	channel string
}

func (f *fakeLiveRooms) LiveRooms(_ context.Context, channelID string) ([]string, error) {
	f.channel = channelID

	return f.rooms, f.err
}

func TestLiveRoomsRouteListsRoomsBehindTheInternalKey(t *testing.T) {
	const channelID = "UC1DCedRgGHBdm81E1llLhOQ"

	for _, tt := range []struct {
		key      string
		resolver *fakeLiveRooms
		want     int
		body     string
	}{
		{"", &fakeLiveRooms{rooms: []string{"1"}}, http.StatusUnauthorized, ""},
		{"live-rooms-key", &fakeLiveRooms{rooms: []string{"1", "2"}}, http.StatusOK, `{"success":true,"data":{"room_ids":["1","2"]}}`},
		{"live-rooms-key", &fakeLiveRooms{rooms: []string{}}, http.StatusOK, `{"success":true,"data":{"room_ids":[]}}`},
		{"live-rooms-key", &fakeLiveRooms{err: errors.New("database unavailable")}, http.StatusInternalServerError, ""},
	} {
		router := gin.New()
		require.NoError(t, NewInternalRouteRegistrar("live-rooms-key", fakeAlarmCRUD{}, eventgrade.NewRepository(nil), tt.resolver, slog.New(slog.DiscardHandler))(router))

		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, contract.LiveRoomsPath(channelID), http.NoBody)
		req.Header.Set("X-API-Key", tt.key)

		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		require.Equal(t, tt.want, rec.Code)

		if tt.body != "" {
			require.JSONEq(t, tt.body, rec.Body.String())
			require.Equal(t, channelID, tt.resolver.channel)
		}
	}
}
