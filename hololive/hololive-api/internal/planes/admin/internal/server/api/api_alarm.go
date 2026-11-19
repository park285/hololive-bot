package api

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/kapu/hololive-shared/pkg/domain"
	sharedprivacylog "github.com/kapu/hololive-shared/pkg/privacylog"
	"github.com/kapu/hololive-shared/pkg/server/ginjson"
	sharedserver "github.com/kapu/hololive-shared/pkg/server/httpserver"
)

type alarmListResponse struct {
	Status string               `json:"status"`
	Alarms []*domain.AlarmEntry `json:"alarms"`
}

type alarmDeleteResponse struct {
	Status  string `json:"status"`
	Removed bool   `json:"removed"`
}

func (h *AlarmHandler) GetAlarms(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), adminRequestTimeout)
	defer cancel()

	alarmKeys, err := h.alarm.GetAllAlarmKeys(ctx)
	if err != nil {
		sharedserver.RespondInternalError(h.safeLogger(), c, "Failed to get alarms", "Failed to get alarm keys", err)

		return
	}

	ginjson.Respond(c, http.StatusOK, alarmListResponse{Status: "ok", Alarms: alarmKeys})
}

func (h *AlarmHandler) DeleteAlarm(c *gin.Context) {
	var req struct {
		RoomID    string `json:"roomId" binding:"required"`
		ChannelID string `json:"channelId" binding:"required"`
	}

	if err := bindJSON(c, &req); err != nil {
		h.safeLogger().Warn("Invalid request body", slog.Any("error", err))
		sharedserver.RespondError(c, http.StatusBadRequest, "invalid request body", nil)

		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), adminRequestTimeout)
	defer cancel()

	removed, err := h.alarm.RemoveAlarm(ctx, req.RoomID, req.ChannelID, nil)
	if err != nil {
		sharedserver.RespondInternalError(h.safeLogger(), c, "Failed to delete alarm", "Failed to delete alarm", err)

		return
	}

	roomToken := sharedprivacylog.IdentifierToken(req.RoomID)
	h.logActivity("alarm_delete", "Alarm deleted", []slog.Attr{
		slog.String("room_id", roomToken),
		slog.String("channel_id", req.ChannelID),
	})

	ginjson.Respond(c, http.StatusOK, alarmDeleteResponse{Status: "ok", Removed: removed})
}
