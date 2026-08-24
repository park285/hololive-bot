package api

import (
	"context"
	jsonv2 "encoding/json/v2"
	"errors"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/park285/iris-client-go/v2/iris"
)

type stubIrisRoomLister struct {
	resp *iris.RoomListResponse
	err  error
}

func (s stubIrisRoomLister) GetRooms(context.Context) (*iris.RoomListResponse, error) {
	if s.err != nil {
		return nil, s.err
	}

	return s.resp, nil
}

const joinedRoomsPath = "/api/holo/rooms/joined"

func TestRoomHandler_GetJoinedRooms(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("missing iris dependency", roomJoinedMissingIris)
	t.Run("lists joined rooms", roomJoinedListsRooms)
	t.Run("iris failure", roomJoinedIrisFailure)
	t.Run("nil iris response", roomJoinedNilResponse)
}

func roomJoinedMissingIris(t *testing.T) {
	handler := &RoomHandler{Handler: &Handler{logger: newDiscardLogger()}}
	ctx, rec := newAPITestContext(http.MethodGet, joinedRoomsPath, nil)

	handler.GetJoinedRooms(ctx)

	assertErrorResponse(t, rec, http.StatusServiceUnavailable, "iris room listing not available")
}

func roomJoinedListsRooms(t *testing.T) {
	name := "운영방"
	roomType := "OM"
	memberCount := 17
	handler := &RoomHandler{Handler: &Handler{
		iris: stubIrisRoomLister{resp: &iris.RoomListResponse{Rooms: []iris.RoomSummary{
			{ChatID: 123456, LinkName: &name, Type: &roomType, ActiveMembersCount: &memberCount},
			{ChatID: 789},
		}}},
		logger: newDiscardLogger(),
	}}
	ctx, rec := newAPITestContext(http.MethodGet, joinedRoomsPath, nil)

	handler.GetJoinedRooms(ctx)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d want=%d body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var got joinedRoomListResponse

	if err := jsonv2.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if got.Status != "ok" || len(got.Rooms) != 2 {
		t.Fatalf("unexpected response: %+v", got)
	}

	if got.Rooms[0] != (joinedRoom{ChatID: "123456", Name: name, Type: roomType, MemberCount: memberCount}) {
		t.Fatalf("unexpected first room: %+v", got.Rooms[0])
	}

	if got.Rooms[1] != (joinedRoom{ChatID: "789"}) {
		t.Fatalf("unexpected second room: %+v", got.Rooms[1])
	}
}

func roomJoinedIrisFailure(t *testing.T) {
	handler := &RoomHandler{Handler: &Handler{
		iris:   stubIrisRoomLister{err: errors.New("iris unavailable")},
		logger: newDiscardLogger(),
	}}
	ctx, rec := newAPITestContext(http.MethodGet, joinedRoomsPath, nil)

	handler.GetJoinedRooms(ctx)

	assertErrorResponse(t, rec, http.StatusBadGateway, "Failed to list joined rooms")
}

func roomJoinedNilResponse(t *testing.T) {
	handler := &RoomHandler{Handler: &Handler{
		iris:   stubIrisRoomLister{},
		logger: newDiscardLogger(),
	}}
	ctx, rec := newAPITestContext(http.MethodGet, joinedRoomsPath, nil)

	handler.GetJoinedRooms(ctx)

	assertErrorResponse(t, rec, http.StatusBadGateway, "Failed to list joined rooms")
}
