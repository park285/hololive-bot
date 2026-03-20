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

package server

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/gin-gonic/gin"
	"github.com/kapu/hololive-shared/pkg/constants"
)

// GetAlarms: 모든 알람을 JSON으로 반환합니다.
func (h *AlarmAPIHandler) GetAlarms(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), constants.RequestTimeout.AdminRequest)
	defer cancel()

	// 모든 알림 레지스트리 키 조회
	alarmKeys, err := h.alarm.GetAllAlarmKeys(ctx)
	if err != nil {
		h.logger.Error("Failed to get alarm keys", slog.Any("error", err))
		c.JSON(500, gin.H{"error": "Failed to get alarms"})

		return
	}

	c.JSON(200, gin.H{
		"status": "ok",
		"alarms": alarmKeys,
	})
}

// DeleteAlarm: 특정 알람을 삭제합니다. (방 기반: room_id + channel_id).
func (h *AlarmAPIHandler) DeleteAlarm(c *gin.Context) {
	var req struct {
		RoomID    string `json:"roomId" binding:"required"`
		ChannelID string `json:"channelId" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		h.logger.Warn("Invalid request body", slog.Any("error", err))
		c.JSON(400, gin.H{"error": "invalid request body"})

		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), constants.RequestTimeout.AdminRequest)
	defer cancel()

	removed, err := h.alarm.RemoveAlarm(ctx, req.RoomID, req.ChannelID, nil)
	if err != nil {
		h.logger.Error("Failed to delete alarm", slog.Any("error", err))
		c.JSON(500, gin.H{"error": "Failed to delete alarm"})

		return
	}

	h.activity.Log("alarm_delete", fmt.Sprintf("Alarm deleted: room=%s channel=%s", req.RoomID, req.ChannelID), map[string]any{
		"room_id":    req.RoomID,
		"channel_id": req.ChannelID,
	})

	c.JSON(200, gin.H{
		"status":  "ok",
		"removed": removed,
	})
}
