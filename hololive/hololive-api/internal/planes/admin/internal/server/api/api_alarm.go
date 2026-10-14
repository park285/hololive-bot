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

package api

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/park285/shared-go/v2/pkg/ginjson"

	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/domain"
	sharedprivacylog "github.com/kapu/hololive-shared/pkg/privacylog"
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
	if !h.requireAlarm(c) {
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), constants.RequestTimeout.AdminRequest)
	defer cancel()

	// 관리 목록은 alarm-worker가 PG에서 (방, 채널) 단위로 만든다.
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

	if !h.requireAlarm(c) {
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), constants.RequestTimeout.AdminRequest)
	defer cancel()

	removed, err := h.alarm.RemoveAlarm(ctx, req.RoomID, req.ChannelID, nil)
	if err != nil {
		sharedserver.RespondInternalError(h.safeLogger(), c, "Failed to delete alarm", "Failed to delete alarm", err)

		return
	}

	roomToken := sharedprivacylog.IdentifierToken(req.RoomID)
	h.logActivity("alarm_delete", fmt.Sprintf("Alarm deleted: room=%s channel=%s", roomToken, req.ChannelID), map[string]any{
		"room_id":    roomToken,
		"channel_id": req.ChannelID,
	})

	ginjson.Respond(c, http.StatusOK, alarmDeleteResponse{Status: "ok", Removed: removed})
}
