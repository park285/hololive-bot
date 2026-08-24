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
	jsonv2 "encoding/json/v2"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"github.com/park285/shared-go/v2/pkg/ginjson"

	contractsalarm "github.com/kapu/hololive-shared/pkg/contracts/alarm"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/privacylog"
)

type Handler struct {
	alarm  domain.AlarmCRUD
	logger *slog.Logger
}

func decodeAlarmRequest(c *gin.Context, destination any) error {
	if err := jsonv2.UnmarshalRead(c.Request.Body, destination); err != nil {
		return fmt.Errorf("unmarshal read: %w", err)
	}

	if binding.Validator == nil {
		return nil
	}

	if err := binding.Validator.ValidateStruct(destination); err != nil {
		return fmt.Errorf("validate struct: %w", err)
	}

	return nil
}

func NewHandler(alarm domain.AlarmCRUD, logger *slog.Logger) *Handler {
	return &Handler{
		alarm:  alarm,
		logger: logger,
	}
}

func (h *Handler) RegisterRoutes(rg *gin.RouterGroup) {
	h.RegisterInternalRoutes(rg)

	rg.GET("/health", h.Health)
	rg.GET("/ready", h.Ready)
}

func (h *Handler) RegisterInternalRoutes(rg *gin.RouterGroup) {
	internal := rg.Group(contractsalarm.BasePath)
	internal.POST(contractsalarm.AddRoute, h.AddAlarm)
	internal.POST(contractsalarm.RemoveRoute, h.RemoveAlarm)
	internal.GET(contractsalarm.RoomRoute, h.GetRoomAlarmsWithTypes)
	internal.GET(contractsalarm.RoomViewRoute, h.GetRoomAlarmsView)
	internal.POST(contractsalarm.ClearRoute, h.ClearRoomAlarms)
	internal.GET(contractsalarm.NextStreamRoute, h.GetNextStreamInfo)
	internal.PUT(contractsalarm.SettingsRoute, h.UpdateAlarmAdvanceMinutes)
	internal.PUT(contractsalarm.RoomNameRoute, h.SetRoomName)
	internal.PUT(contractsalarm.UserNameRoute, h.SetUserName)
	internal.GET(contractsalarm.KeysRoute, h.GetAllAlarmKeys)
}

func (h *Handler) AddAlarm(c *gin.Context) {
	var req AddAlarmRequest

	if err := decodeAlarmRequest(c, &req); err != nil {
		ginjson.Respond(c, http.StatusBadRequest, alarmAPIError("invalid_request_body", err.Error()))

		return
	}

	ctx := c.Request.Context()

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

	added, err := h.alarm.AddAlarm(ctx, &domainReq)
	if err != nil {
		h.logger.Error("알람 추가 실패", slog.Any("error", err))
		ginjson.Respond(c, http.StatusInternalServerError, alarmAPIError("alarm_add_failed", "alarm add failed"))

		return
	}

	ginjson.Respond(c, http.StatusOK, APIResponse{Success: true, Data: gin.H{"added": added}})
}

func (h *Handler) RemoveAlarm(c *gin.Context) {
	var req RemoveAlarmRequest

	if err := decodeAlarmRequest(c, &req); err != nil {
		ginjson.Respond(c, http.StatusBadRequest, alarmAPIError("invalid_request_body", err.Error()))

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
		ginjson.Respond(c, http.StatusInternalServerError, alarmAPIError("alarm_remove_failed", "alarm remove failed"))

		return
	}

	ginjson.Respond(c, http.StatusOK, APIResponse{Success: true, Data: gin.H{"removed": removed}})
}

func (h *Handler) GetRoomAlarmsWithTypes(c *gin.Context) {
	roomID := c.Param("id")
	ctx := c.Request.Context()

	alarms, err := h.alarm.GetRoomAlarmsWithTypes(ctx, roomID)
	if err != nil {
		h.logger.Error("방 알람 조회 실패", privacylog.RoomIDAttr(roomID), slog.Any("error", err))
		ginjson.Respond(c, http.StatusInternalServerError, alarmAPIError("get_room_alarms_failed", "get room alarms failed"))

		return
	}

	ginjson.Respond(c, http.StatusOK, APIResponse{Success: true, Data: alarms})
}

func (h *Handler) GetRoomAlarmsView(c *gin.Context) {
	roomID := c.Param("id")
	ctx := c.Request.Context()

	entries, err := h.alarm.ListRoomAlarmsView(ctx, roomID)
	if err != nil {
		h.logger.Error("방 알람 표시 조회 실패", privacylog.RoomIDAttr(roomID), slog.Any("error", err))
		ginjson.Respond(c, http.StatusInternalServerError, alarmAPIError("get_room_alarms_view_failed", "get room alarms view failed"))

		return
	}

	ginjson.Respond(c, http.StatusOK, APIResponse{Success: true, Data: entries})
}

func (h *Handler) ClearRoomAlarms(c *gin.Context) {
	var req ClearAlarmsRequest

	if err := decodeAlarmRequest(c, &req); err != nil {
		ginjson.Respond(c, http.StatusBadRequest, alarmAPIError("invalid_request_body", err.Error()))

		return
	}

	ctx := c.Request.Context()

	count, err := h.alarm.ClearRoomAlarms(ctx, req.RoomID)
	if err != nil {
		h.logger.Error("방 알람 전체 삭제 실패", privacylog.RoomIDAttr(req.RoomID), slog.Any("error", err))
		ginjson.Respond(c, http.StatusInternalServerError, alarmAPIError("clear_room_alarms_failed", "clear room alarms failed"))

		return
	}

	ginjson.Respond(c, http.StatusOK, APIResponse{Success: true, Data: gin.H{"deleted": count}})
}

func (h *Handler) GetNextStreamInfo(c *gin.Context) {
	channelID := c.Param("id")
	ctx := c.Request.Context()

	info, err := h.alarm.GetNextStreamInfo(ctx, channelID)
	if err != nil {
		h.logger.Error("다음 방송 정보 조회 실패", slog.String("channel_id", channelID), slog.Any("error", err))
		ginjson.Respond(c, http.StatusInternalServerError, alarmAPIError("get_next_stream_info_failed", "get next stream info failed"))

		return
	}

	if info == nil {
		ginjson.Respond(c, http.StatusOK, APIResponse{Success: true, Data: nil})

		return
	}

	ginjson.Respond(c, http.StatusOK, APIResponse{Success: true, Data: info})
}

func (h *Handler) UpdateAlarmAdvanceMinutes(c *gin.Context) {
	var req UpdateAdvanceMinutesRequest

	if err := decodeAlarmRequest(c, &req); err != nil {
		ginjson.Respond(c, http.StatusBadRequest, alarmAPIError("invalid_request_body", err.Error()))

		return
	}

	targets := h.alarm.UpdateAlarmAdvanceMinutes(c.Request.Context(), req.Minutes)
	ginjson.Respond(c, http.StatusOK, APIResponse{Success: true, Data: gin.H{"target_minutes": targets}})
}

func (h *Handler) SetRoomName(c *gin.Context) {
	var req SetRoomNameRequest

	if err := decodeAlarmRequest(c, &req); err != nil {
		ginjson.Respond(c, http.StatusBadRequest, alarmAPIError("invalid_request_body", err.Error()))

		return
	}

	ctx := c.Request.Context()

	if err := h.alarm.SetRoomName(ctx, req.RoomID, req.RoomName); err != nil {
		h.logger.Error("방 이름 설정 실패", privacylog.RoomIDAttr(req.RoomID), slog.Any("error", err))
		ginjson.Respond(c, http.StatusInternalServerError, alarmAPIError("set_room_name_failed", "set room name failed"))

		return
	}

	ginjson.Respond(c, http.StatusOK, APIResponse{Success: true})
}

func (h *Handler) SetUserName(c *gin.Context) {
	var req SetUserNameRequest

	if err := decodeAlarmRequest(c, &req); err != nil {
		ginjson.Respond(c, http.StatusBadRequest, alarmAPIError("invalid_request_body", err.Error()))

		return
	}

	ctx := c.Request.Context()

	if err := h.alarm.SetUserName(ctx, req.UserID, req.UserName); err != nil {
		h.logger.Error("사용자 이름 설정 실패", slog.String("user_id", req.UserID), slog.Any("error", err))
		ginjson.Respond(c, http.StatusInternalServerError, alarmAPIError("set_user_name_failed", "set user name failed"))

		return
	}

	ginjson.Respond(c, http.StatusOK, APIResponse{Success: true})
}

func (h *Handler) GetAllAlarmKeys(c *gin.Context) {
	ctx := c.Request.Context()

	keys, err := h.alarm.GetAllAlarmKeys(ctx)
	if err != nil {
		h.logger.Error("알람 키 전체 조회 실패", slog.Any("error", err))
		ginjson.Respond(c, http.StatusInternalServerError, alarmAPIError("get_all_alarm_keys_failed", "get all alarm keys failed"))

		return
	}

	ginjson.Respond(c, http.StatusOK, APIResponse{Success: true, Data: keys})
}

func (h *Handler) Health(c *gin.Context) {
	ginjson.Respond(c, http.StatusOK, gin.H{"status": "ok"})
}

func (h *Handler) Ready(c *gin.Context) {
	ginjson.Respond(c, http.StatusOK, gin.H{"status": "ready"})
}
