package api

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/kapu/hololive-shared/pkg/privacylog"
	"github.com/kapu/hololive-shared/pkg/server/ginjson"
	sharedserver "github.com/kapu/hololive-shared/pkg/server/httpserver"
	"github.com/kapu/hololive-shared/pkg/service/kakaoroom"
)

type DeliveryPauseStore interface {
	List(ctx context.Context) ([]kakaoroom.DeliveryPause, error)
	Resume(ctx context.Context, roomID string) (bool, error)
}

type deliveryPauseView struct {
	RoomID   string    `json:"roomId"`
	Reason   string    `json:"reason"`
	PausedAt time.Time `json:"pausedAt"`
}

type deliveryPauseListResponse struct {
	Status string              `json:"status"`
	Pauses []deliveryPauseView `json:"pauses"`
}

type deliveryPauseResumeResponse struct {
	Status  string `json:"status"`
	Resumed bool   `json:"resumed"`
}

func (h *RoomHandler) GetDeliveryPauses(c *gin.Context) {
	if !h.requireDeliveryPauses(c) {
		return
	}

	pauses, err := h.pauses.List(c.Request.Context())
	if err != nil {
		h.safeLogger().Error("Failed to list kakao room delivery pauses", slog.Any("error", err))
		sharedserver.RespondError(c, http.StatusInternalServerError, "Failed to list room delivery pauses", nil)

		return
	}

	views := make([]deliveryPauseView, 0, len(pauses))
	for _, pause := range pauses {
		views = append(views, deliveryPauseView{RoomID: pause.RoomID, Reason: pause.Reason, PausedAt: pause.PausedAt.UTC()})
	}

	ginjson.Respond(c, http.StatusOK, deliveryPauseListResponse{Status: "ok", Pauses: views})
}

func (h *RoomHandler) ResumeDeliveryPause(c *gin.Context) {
	if !h.requireDeliveryPauses(c) {
		return
	}

	roomID := strings.TrimSpace(c.Param("roomId"))
	if _, err := strconv.ParseInt(roomID, 10, 64); err != nil {
		sharedserver.RespondError(c, http.StatusBadRequest, "room must be a chat ID (signed 64-bit integer)", nil)

		return
	}

	resumed, err := h.pauses.Resume(c.Request.Context(), roomID)
	if err != nil {
		h.safeLogger().Error("Failed to resume kakao room delivery", privacylog.RoomIDAttr(roomID), slog.Any("error", err))
		sharedserver.RespondError(c, http.StatusInternalServerError, "Failed to resume room delivery", nil)

		return
	}

	if resumed {
		h.logActivity("room_delivery_resume", "Room delivery resumed: ", []slog.Attr{privacylog.RoomIDAttr(roomID)})
	}

	ginjson.Respond(c, http.StatusOK, deliveryPauseResumeResponse{Status: "ok", Resumed: resumed})
}

func (h *RoomHandler) requireDeliveryPauses(c *gin.Context) bool {
	if h == nil || h.pauses == nil {
		respondServiceUnavailable(c, "room delivery pauses not available")

		return false
	}

	return true
}
