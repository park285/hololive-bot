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
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/kapu/hololive-shared/pkg/domain"
)

// APIHandler: alarm-dispatcher internal API 핸들러
type APIHandler struct {
	alarm  domain.AlarmCRUD
	logger *slog.Logger
}

// NewAPIHandler: 새로운 APIHandler를 생성합니다.
func NewAPIHandler(alarm domain.AlarmCRUD, logger *slog.Logger) *APIHandler {
	return &APIHandler{
		alarm:  alarm,
		logger: logger,
	}
}

// RegisterRoutes: 라우터 그룹에 알람 internal API 엔드포인트를 등록합니다.
func (h *APIHandler) RegisterRoutes(rg *gin.RouterGroup) {
	h.RegisterInternalRoutes(rg)

	rg.GET("/health", h.Health)
	rg.GET("/ready", h.Ready)
}

// RegisterInternalRoutes: 알람 internal API 엔드포인트만 등록합니다. (/health, /ready 제외)
func (h *APIHandler) RegisterInternalRoutes(rg *gin.RouterGroup) {
	internal := rg.Group("/internal/alarm")
	internal.POST("/add", h.AddAlarm)
	internal.POST("/remove", h.RemoveAlarm)
	internal.GET("/room/:id", h.GetRoomAlarmsWithTypes)
	internal.GET("/room/:id/view", h.GetRoomAlarmsView)
	internal.POST("/clear", h.ClearRoomAlarms)
	internal.GET("/next-stream/:id", h.GetNextStreamInfo)
	internal.PUT("/settings", h.UpdateAlarmAdvanceMinutes)
	internal.PUT("/room-name", h.SetRoomName)
	internal.PUT("/user-name", h.SetUserName)
	internal.GET("/keys", h.GetAllAlarmKeys)
}

// AddAlarm: 알람을 추가합니다.
func (h *APIHandler) AddAlarm(c *gin.Context) {
	var req AddAlarmRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, APIResponse{Success: false, Message: err.Error()})
		return
	}

	ctx := c.Request.Context()

	// []string → domain.AlarmTypes 변환
	alarmTypes := make(domain.AlarmTypes, 0, len(req.AlarmTypes))
	for _, t := range req.AlarmTypes {
		alarmTypes = append(alarmTypes, domain.AlarmType(t))
	}

	domainReq := domain.AddAlarmRequest{
		RoomID:     req.RoomID,
		UserID:     req.UserID,
		ChannelID:  req.ChannelID,
		MemberName: req.MemberName,
		RoomName:   req.RoomName,
		UserName:   req.UserName,
		AlarmTypes: alarmTypes,
	}

	added, err := h.alarm.AddAlarm(ctx, domainReq)
	if err != nil {
		h.logger.Error("알람 추가 실패", slog.Any("error", err))
		c.JSON(http.StatusInternalServerError, APIResponse{Success: false, Message: "alarm add failed"})
		return
	}

	c.JSON(http.StatusOK, APIResponse{Success: true, Data: gin.H{"added": added}})
}

// RemoveAlarm: 알람을 제거합니다.
func (h *APIHandler) RemoveAlarm(c *gin.Context) {
	var req RemoveAlarmRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, APIResponse{Success: false, Message: err.Error()})
		return
	}

	ctx := c.Request.Context()

	alarmTypes := make(domain.AlarmTypes, 0, len(req.AlarmTypes))
	for _, t := range req.AlarmTypes {
		alarmTypes = append(alarmTypes, domain.AlarmType(t))
	}

	removed, err := h.alarm.RemoveAlarm(ctx, req.RoomID, req.ChannelID, alarmTypes)
	if err != nil {
		h.logger.Error("알람 제거 실패", slog.Any("error", err))
		c.JSON(http.StatusInternalServerError, APIResponse{Success: false, Message: "alarm remove failed"})
		return
	}

	c.JSON(http.StatusOK, APIResponse{Success: true, Data: gin.H{"removed": removed}})
}

// GetRoomAlarmsWithTypes: 방의 알람 목록(타입 포함)을 조회합니다.
func (h *APIHandler) GetRoomAlarmsWithTypes(c *gin.Context) {
	roomID := c.Param("id")
	ctx := c.Request.Context()

	alarms, err := h.alarm.GetRoomAlarmsWithTypes(ctx, roomID)
	if err != nil {
		h.logger.Error("방 알람 조회 실패", slog.String("room_id", roomID), slog.Any("error", err))
		c.JSON(http.StatusInternalServerError, APIResponse{Success: false, Message: "get room alarms failed"})
		return
	}

	c.JSON(http.StatusOK, APIResponse{Success: true, Data: alarms})
}

// GetRoomAlarmsView: 방의 알람 목록 표시용 조합 조회 결과를 반환합니다.
func (h *APIHandler) GetRoomAlarmsView(c *gin.Context) {
	roomID := c.Param("id")
	ctx := c.Request.Context()

	entries, err := h.alarm.ListRoomAlarmsView(ctx, roomID)
	if err != nil {
		h.logger.Error("방 알람 표시 조회 실패", slog.String("room_id", roomID), slog.Any("error", err))
		c.JSON(http.StatusInternalServerError, APIResponse{Success: false, Message: "get room alarms view failed"})
		return
	}

	c.JSON(http.StatusOK, APIResponse{Success: true, Data: entries})
}

// ClearRoomAlarms: 방의 모든 알람을 삭제합니다.
func (h *APIHandler) ClearRoomAlarms(c *gin.Context) {
	var req ClearAlarmsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, APIResponse{Success: false, Message: err.Error()})
		return
	}

	ctx := c.Request.Context()

	count, err := h.alarm.ClearRoomAlarms(ctx, req.RoomID)
	if err != nil {
		h.logger.Error("방 알람 전체 삭제 실패", slog.String("room_id", req.RoomID), slog.Any("error", err))
		c.JSON(http.StatusInternalServerError, APIResponse{Success: false, Message: "clear room alarms failed"})
		return
	}

	c.JSON(http.StatusOK, APIResponse{Success: true, Data: gin.H{"deleted": count}})
}

// GetNextStreamInfo: 채널의 다음 방송 정보를 조회합니다.
func (h *APIHandler) GetNextStreamInfo(c *gin.Context) {
	channelID := c.Param("id")
	ctx := c.Request.Context()

	info, err := h.alarm.GetNextStreamInfo(ctx, channelID)
	if err != nil {
		h.logger.Error("다음 방송 정보 조회 실패", slog.String("channel_id", channelID), slog.Any("error", err))
		c.JSON(http.StatusInternalServerError, APIResponse{Success: false, Message: "get next stream info failed"})
		return
	}

	// nil이면 예정 방송 없음
	if info == nil {
		c.JSON(http.StatusOK, APIResponse{Success: true, Data: nil})
		return
	}

	c.JSON(http.StatusOK, APIResponse{Success: true, Data: info})
}

// UpdateAlarmAdvanceMinutes: 알림 발송 시간(분)을 업데이트합니다.
func (h *APIHandler) UpdateAlarmAdvanceMinutes(c *gin.Context) {
	var req UpdateAdvanceMinutesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, APIResponse{Success: false, Message: err.Error()})
		return
	}

	targets := h.alarm.UpdateAlarmAdvanceMinutes(c.Request.Context(), req.Minutes)
	c.JSON(http.StatusOK, APIResponse{Success: true, Data: gin.H{"target_minutes": targets}})
}

// SetRoomName: 방 이름을 설정합니다.
func (h *APIHandler) SetRoomName(c *gin.Context) {
	var req SetRoomNameRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, APIResponse{Success: false, Message: err.Error()})
		return
	}

	ctx := c.Request.Context()

	if err := h.alarm.SetRoomName(ctx, req.RoomID, req.RoomName); err != nil {
		h.logger.Error("방 이름 설정 실패", slog.String("room_id", req.RoomID), slog.Any("error", err))
		c.JSON(http.StatusInternalServerError, APIResponse{Success: false, Message: "set room name failed"})
		return
	}

	c.JSON(http.StatusOK, APIResponse{Success: true})
}

// SetUserName: 사용자 이름을 설정합니다.
func (h *APIHandler) SetUserName(c *gin.Context) {
	var req SetUserNameRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, APIResponse{Success: false, Message: err.Error()})
		return
	}

	ctx := c.Request.Context()

	if err := h.alarm.SetUserName(ctx, req.UserID, req.UserName); err != nil {
		h.logger.Error("사용자 이름 설정 실패", slog.String("user_id", req.UserID), slog.Any("error", err))
		c.JSON(http.StatusInternalServerError, APIResponse{Success: false, Message: "set user name failed"})
		return
	}

	c.JSON(http.StatusOK, APIResponse{Success: true})
}

// GetAllAlarmKeys: 모든 알람 키 목록을 조회합니다.
func (h *APIHandler) GetAllAlarmKeys(c *gin.Context) {
	ctx := c.Request.Context()

	keys, err := h.alarm.GetAllAlarmKeys(ctx)
	if err != nil {
		h.logger.Error("알람 키 전체 조회 실패", slog.Any("error", err))
		c.JSON(http.StatusInternalServerError, APIResponse{Success: false, Message: "get all alarm keys failed"})
		return
	}

	c.JSON(http.StatusOK, APIResponse{Success: true, Data: keys})
}

// Health: liveness probe — 서비스가 살아있는지 확인합니다.
func (h *APIHandler) Health(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// Ready: readiness probe — 트래픽을 받을 준비가 됐는지 확인합니다.
func (h *APIHandler) Ready(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ready"})
}
