package api

import (
	"context"
	jsonv2 "encoding/json/v2"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/kapu/hololive-shared/pkg/service/kakaoroom"
)

type stubDeliveryPauses struct {
	pauses  []kakaoroom.DeliveryPause
	resumed map[string]bool
	err     error
	calls   []string
}

func (s *stubDeliveryPauses) List(context.Context) ([]kakaoroom.DeliveryPause, error) {
	return s.pauses, s.err
}

func (s *stubDeliveryPauses) Resume(_ context.Context, roomID string) (bool, error) {
	s.calls = append(s.calls, roomID)

	return s.resumed[roomID], s.err
}

const deliveryPausesPath = "/api/holo/rooms/delivery-pauses"

func TestRoomHandlerDeliveryPauses(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("missing store", deliveryPausesMissingStore)
	t.Run("lists paused rooms", deliveryPausesList)
	t.Run("list failure", deliveryPausesListFailure)
	t.Run("resume validates the chat id before touching the store", deliveryPausesRejectInvalidRoom)
	t.Run("resume paused room", deliveryPausesResume(true))
	t.Run("resume room that is not paused", deliveryPausesResume(false))
}

func deliveryPausesMissingStore(t *testing.T) {
	handler := &RoomHandler{logger: newDiscardLogger()}
	ctx, rec := newAPITestContext(http.MethodGet, deliveryPausesPath, nil)

	handler.GetDeliveryPauses(ctx)

	assertErrorResponse(t, rec, http.StatusServiceUnavailable, "room delivery pauses not available")
}

func deliveryPausesList(t *testing.T) {
	pausedAt := time.Date(2026, time.November, 25, 1, 2, 3, 0, time.FixedZone("KST", 9*60*60))
	store := &stubDeliveryPauses{pauses: []kakaoroom.DeliveryPause{{RoomID: "123", Reason: "room_not_joined", PausedAt: pausedAt}}}
	handler := &RoomHandler{pauses: store, logger: newDiscardLogger()}
	ctx, rec := newAPITestContext(http.MethodGet, deliveryPausesPath, nil)

	handler.GetDeliveryPauses(ctx)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}

	var got deliveryPauseListResponse

	if err := jsonv2.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	want := deliveryPauseView{RoomID: "123", Reason: "room_not_joined", PausedAt: pausedAt.UTC()}
	if got.Status != "ok" || len(got.Pauses) != 1 || got.Pauses[0] != want {
		t.Fatalf("response = %+v, want one %+v", got, want)
	}
}

func deliveryPausesListFailure(t *testing.T) {
	handler := &RoomHandler{pauses: &stubDeliveryPauses{err: errors.New("db down")}, logger: newDiscardLogger()}
	ctx, rec := newAPITestContext(http.MethodGet, deliveryPausesPath, nil)

	handler.GetDeliveryPauses(ctx)

	assertErrorResponse(t, rec, http.StatusInternalServerError, "Failed to list room delivery pauses")
}

func deliveryPausesRejectInvalidRoom(t *testing.T) {
	store := &stubDeliveryPauses{}
	handler := &RoomHandler{pauses: store, logger: newDiscardLogger()}
	ctx, rec := newAPITestContext(http.MethodDelete, deliveryPausesPath+"/room", nil)

	ctx.Params = gin.Params{{Key: "roomId", Value: "room"}}

	handler.ResumeDeliveryPause(ctx)

	assertErrorResponse(t, rec, http.StatusBadRequest, "room must be a chat ID (signed 64-bit integer)")

	if len(store.calls) != 0 {
		t.Fatalf("store calls = %v, want none", store.calls)
	}
}

func deliveryPausesResume(resumed bool) func(*testing.T) {
	return func(t *testing.T) {
		store := &stubDeliveryPauses{resumed: map[string]bool{"-42": resumed}}
		handler := &RoomHandler{pauses: store, activity: newActivityLoggerForTest(t), logger: newDiscardLogger()}
		ctx, rec := newAPITestContext(http.MethodDelete, deliveryPausesPath+"/-42", nil)

		ctx.Params = gin.Params{{Key: "roomId", Value: "-42"}}

		handler.ResumeDeliveryPause(ctx)

		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
		}

		var got deliveryPauseResumeResponse

		if err := jsonv2.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("decode response: %v", err)
		}

		if got != (deliveryPauseResumeResponse{Status: "ok", Resumed: resumed}) || len(store.calls) != 1 || store.calls[0] != "-42" {
			t.Fatalf("response=%+v calls=%v", got, store.calls)
		}
	}
}
