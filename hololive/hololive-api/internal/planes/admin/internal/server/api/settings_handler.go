package api

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/park285/shared-go/v2/pkg/ginjson"

	sharedsettings "github.com/kapu/hololive-api/internal/server/settings"
	"github.com/kapu/hololive-shared/pkg/constants"
	contractssettings "github.com/kapu/hololive-shared/pkg/contracts/settings"
	"github.com/kapu/hololive-shared/pkg/domain"
	sharedserver "github.com/kapu/hololive-shared/pkg/server/httpserver"
	sharedchecker "github.com/kapu/hololive-shared/pkg/service/alarm/checker"
	settingssvc "github.com/kapu/hololive-shared/pkg/service/settings"
)

type logsResponse struct {
	Status string `json:"status"`
	Logs   any    `json:"logs"`
}

type settingsResponse struct {
	Status   string               `json:"status"`
	Settings settingssvc.Settings `json:"settings"`
	Runtime  map[string]any       `json:"runtime"`
}

type settingsUpdateResponse struct {
	Status   string               `json:"status"`
	Message  string               `json:"message"`
	Settings settingssvc.Settings `json:"settings"`
	Runtime  map[string]any       `json:"runtime"`
}

type llmSettingsResponse struct {
	Status  string         `json:"status"`
	Message string         `json:"message"`
	Runtime map[string]any `json:"runtime"`
}

type SettingsActivityLogger interface {
	Log(entryType, summary string, details map[string]any)
}

type SettingsReadRecentLogsFunc func(limit int) (any, error)

type SettingsHandler struct {
	sharedsettings.SettingsApplier

	Logger         *slog.Logger
	Alarm          domain.AlarmCRUD
	Activity       SettingsActivityLogger
	ReadRecentLogs SettingsReadRecentLogsFunc
	Settings       settingssvc.ReadWriter
}

// updateSettingsRequest의 scraperProxyEnabled 필드는 DEC-20260926-hololive-legacy-env-config-retirement로 지웠다. 소비자인
// iris-console이 먼저 이 필드를 보내지 않고 요청에 넣으면 거절하도록 바뀌었다.
type updateSettingsRequest struct {
	AlarmAdvanceMinutes *int `json:"alarmAdvanceMinutes"`
}

type updateLLMSettingsRequest struct {
	MemberNewsWeeklyRunNow *bool `json:"memberNewsWeeklyRunNow"`
}

const (
	minAlarmAdvanceMinutes = 0
	maxAlarmAdvanceMinutes = 24 * 60
)

func (h *SettingsHandler) safeLogger() *slog.Logger {
	if h == nil {
		return slog.Default()
	}

	return loggerOrDefault(h.Logger)
}

func (h *SettingsHandler) logActivity(entryType, summary string, details map[string]any) {
	if h != nil && h.Activity != nil {
		h.Activity.Log(entryType, summary, details)
	}
}

func (h *SettingsHandler) requireAlarm(c *gin.Context) bool {
	if h == nil || h.Alarm == nil {
		respondServiceUnavailable(c, "alarm service not available")

		return false
	}

	return true
}

func (h *SettingsHandler) requireSettings(c *gin.Context) bool {
	if h == nil || h.Settings == nil {
		respondServiceUnavailable(c, "settings service not available")

		return false
	}

	return true
}

func (h *SettingsHandler) requireApplier(c *gin.Context) bool {
	if h == nil || h.SettingsApplier == nil {
		respondServiceUnavailable(c, "settings applier not available")

		return false
	}

	return true
}

// SetRoomName은 관리자 지정 방 이름을 저장한다. 요청 필드 roomName은 필수이고, 공백뿐인 값은 지정을 해제해 관리 목록이
// Kakao 방 이름으로 돌아가게 한다.
func (h *SettingsHandler) SetRoomName(c *gin.Context) {
	var req struct {
		RoomID   string  `json:"roomId" binding:"required"`
		RoomName *string `json:"roomName" binding:"required"`
	}

	if err := bindJSON(c, &req); err != nil {
		h.safeLogger().Warn("Invalid request body", slog.Any("error", err))
		sharedserver.RespondError(c, 400, "invalid request body", nil)

		return
	}

	if !h.requireAlarm(c) {
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), constants.RequestTimeout.AdminRequest)
	defer cancel()

	roomName := strings.TrimSpace(*req.RoomName)
	if err := h.Alarm.SetRoomName(ctx, req.RoomID, roomName); err != nil {
		h.safeLogger().Error("Failed to set room name", slog.Any("error", err))
		sharedserver.RespondError(c, 500, "Failed to set room name", nil)

		return
	}

	if roomName == "" {
		h.safeLogger().Info("Room name cleared", slog.String("room_id", req.RoomID))
		h.logActivity("name_update", "Room name cleared: "+req.RoomID, map[string]any{"room_id": req.RoomID})
		ginjson.Respond(c, 200, statusMessageResponse{Status: "ok", Message: "Room name cleared"})

		return
	}

	h.safeLogger().Info("Room name set",
		slog.String("room_id", req.RoomID),
		slog.String("room_name", roomName),
	)

	h.logActivity("name_update", fmt.Sprintf("Room name set: %s -> %s", req.RoomID, roomName), map[string]any{
		"room_id":   req.RoomID,
		"room_name": roomName,
	})

	ginjson.Respond(c, 200, statusMessageResponse{Status: "ok", Message: "Room name set successfully"})
}

func (h *SettingsHandler) GetLogs(c *gin.Context) {
	if h == nil || h.ReadRecentLogs == nil {
		sharedserver.RespondError(c, http.StatusServiceUnavailable, "activity log service not available", nil)

		return
	}

	logs, err := h.ReadRecentLogs(100)
	if err != nil {
		h.safeLogger().Error("Failed to get logs", slog.Any("error", err))
		sharedserver.RespondError(c, 500, "Failed to get logs", nil)

		return
	}

	ginjson.Respond(c, 200, logsResponse{Status: "ok", Logs: logs})
}

func (h *SettingsHandler) GetSettings(c *gin.Context) {
	if !h.requireSettings(c) || !h.requireApplier(c) {
		return
	}

	s := h.Settings.Get()
	runtime := h.SettingsRuntimeState().AsMap()
	ginjson.Respond(c, 200, settingsResponse{Status: "ok", Settings: s, Runtime: runtime})
}

func (h *SettingsHandler) UpdateSettings(c *gin.Context) {
	req, ok := h.bindUpdateSettingsRequest(c)
	if !ok {
		return
	}

	if !h.requireSettings(c) || !h.requireApplier(c) {
		return
	}

	current := h.Settings.Get()
	alarmAdvanceUpdated := req.applyTo(&current)

	if err := h.Settings.Update(current); err != nil {
		h.safeLogger().Error("Failed to update settings", slog.Any("error", err))
		sharedserver.RespondError(c, 500, "Failed to update settings", nil)

		return
	}

	runtime := h.applySettingsRuntime(c.Request.Context(), current, alarmAdvanceUpdated)
	h.logSettingsUpdate(current, runtime)

	ginjson.Respond(c, 200, settingsUpdateResponse{Status: "ok", Message: "Settings updated", Settings: current, Runtime: runtime})
}

func (h *SettingsHandler) bindUpdateSettingsRequest(c *gin.Context) (updateSettingsRequest, bool) {
	var req updateSettingsRequest

	if err := bindJSON(c, &req); err != nil {
		h.safeLogger().Warn("Invalid request body", slog.Any("error", err))
		sharedserver.RespondError(c, 400, "invalid request body", nil)

		return req, false
	}

	if !validAlarmAdvanceMinutes(req.AlarmAdvanceMinutes) {
		sharedserver.RespondError(
			c,
			400,
			fmt.Sprintf("alarmAdvanceMinutes must be between %d and %d", minAlarmAdvanceMinutes, maxAlarmAdvanceMinutes),
			nil,
		)

		return req, false
	}

	return req, true
}

func validAlarmAdvanceMinutes(minutes *int) bool {
	return minutes == nil || (*minutes >= minAlarmAdvanceMinutes && *minutes <= maxAlarmAdvanceMinutes)
}

func (req updateSettingsRequest) applyTo(current *settingssvc.Settings) bool {
	alarmAdvanceUpdated := req.AlarmAdvanceMinutes != nil
	if alarmAdvanceUpdated {
		current.AlarmAdvanceMinutes = *req.AlarmAdvanceMinutes
		current.TargetMinutes = sharedchecker.BuildRuntimeTargetMinutes(*req.AlarmAdvanceMinutes)
	}

	return alarmAdvanceUpdated
}

func (h *SettingsHandler) applySettingsRuntime(ctx context.Context, current settingssvc.Settings, alarmAdvanceUpdated bool) map[string]any {
	runtime := map[string]any{}

	if alarmAdvanceUpdated {
		alarmAdvanceResult := h.ApplyAlarmAdvanceMinutes(ctx, current.AlarmAdvanceMinutes)
		maps.Copy(runtime, alarmAdvanceResult.AsMap())
	}

	return runtime
}

func (h *SettingsHandler) logSettingsUpdate(current settingssvc.Settings, runtime map[string]any) {
	h.logActivity("settings_update", "Settings updated", map[string]any{
		"alarm_advance_minutes": current.AlarmAdvanceMinutes,
		"runtime_status":        runtime,
	})
}

func (h *SettingsHandler) UpdateLLMSettings(c *gin.Context) {
	req, ok := h.bindUpdateLLMSettingsRequest(c)
	if !ok {
		return
	}

	if !h.requireApplier(c) {
		return
	}

	if !req.validate(c) {
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), constants.RequestTimeout.AdminRequest)
	defer cancel()

	runtime := map[string]any{}

	if req.MemberNewsWeeklyRunNow != nil && *req.MemberNewsWeeklyRunNow {
		memberNewsResult := h.ApplyMemberNewsWeeklyRunNow(ctx)

		runtime[contractssettings.UpdateTypeMemberNewsRunNow] = memberNewsResult.AsMap()
	}

	h.logActivity("llm_settings_update", "LLM settings updated", map[string]any{
		contractssettings.UpdateTypeMemberNewsRunNow: req.MemberNewsWeeklyRunNow,
		"runtime": runtime,
	})

	ginjson.Respond(c, 200, llmSettingsResponse{
		Status:  "ok",
		Message: "LLM settings updated",
		Runtime: runtime,
	})
}

func (h *SettingsHandler) bindUpdateLLMSettingsRequest(c *gin.Context) (updateLLMSettingsRequest, bool) {
	var req updateLLMSettingsRequest

	if err := bindJSON(c, &req); err != nil {
		h.safeLogger().Warn("Invalid request body", slog.Any("error", err))
		sharedserver.RespondError(c, 400, "invalid request body", nil)

		return req, false
	}

	return req, true
}

func (req updateLLMSettingsRequest) validate(c *gin.Context) bool {
	if req.MemberNewsWeeklyRunNow == nil {
		sharedserver.RespondError(c, 400, "at least one llm setting field is required", nil)

		return false
	}

	if req.MemberNewsWeeklyRunNow != nil && !*req.MemberNewsWeeklyRunNow {
		sharedserver.RespondError(c, 400, "memberNewsWeeklyRunNow must be true when provided", nil)

		return false
	}

	return true
}
