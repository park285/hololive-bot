package api

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/park285/shared-go/v2/pkg/ginjson"

	sharedsettings "github.com/kapu/hololive-api/internal/server/settings"
	"github.com/kapu/hololive-shared/pkg/alarmtiming/targetpolicy"
	"github.com/kapu/hololive-shared/pkg/constants"
	contractsalarm "github.com/kapu/hololive-shared/pkg/contracts/alarm"
	contractssettings "github.com/kapu/hololive-shared/pkg/contracts/settings"
	sharedserver "github.com/kapu/hololive-shared/pkg/server/httpserver"
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

// RoomNameSetter는 관리 화면이 알림 서비스에 요구하는 방 이름 변경 작업만 제공한다.
type RoomNameSetter interface {
	SetRoomName(ctx context.Context, roomID, roomName string) error
}

type SettingsHandler struct {
	sharedsettings.SettingsApplier

	Logger         *slog.Logger
	Alarm          RoomNameSetter
	Activity       SettingsActivityLogger
	ReadRecentLogs SettingsReadRecentLogsFunc
	Settings       settingssvc.ReadWriter

	operationGate *settingsOperationGate
	operations    settingsOperationGate
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
	minAlarmAdvanceMinutes = 1
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
// Kakao 방 이름으로 돌아가게 한다. 저장 폭(room_id 100자, 이름 255자)을 넘으면 worker 호출 전에 400으로 거절한다.
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

	roomID, roomName, err := contractsalarm.NormalizeRoomName(req.RoomID, *req.RoomName)
	if err != nil {
		h.safeLogger().Warn("Invalid room name request", slog.Any("error", err))

		sharedserver.RespondError(c, 400, "invalid request body", nil)

		return
	}

	if !h.requireAlarm(c) {
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), constants.RequestTimeout.AdminRequest)

	defer cancel()

	if err := h.Alarm.SetRoomName(ctx, roomID, roomName); err != nil {
		h.safeLogger().Error("Failed to set room name", slog.Any("error", err))

		sharedserver.RespondError(c, 500, "Failed to set room name", nil)

		return
	}

	if roomName == "" {
		h.safeLogger().Info("Room name cleared", slog.String("room_id", roomID))

		h.logActivity("name_update", "Room name cleared: "+roomID, map[string]any{"room_id": roomID})

		ginjson.Respond(c, 200, statusMessageResponse{Status: "ok", Message: "Room name cleared"})

		return
	}

	h.safeLogger().Info("Room name set",
		slog.String("room_id", roomID),
		slog.String("room_name", roomName),
	)

	h.logActivity("name_update", fmt.Sprintf("Room name set: %s -> %s", roomID, roomName), map[string]any{
		"room_id":   roomID,
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

	ctx, cancel := context.WithTimeout(c.Request.Context(), constants.RequestTimeout.AdminRequest)

	defer cancel()

	release, err := h.settingsOperationGate().acquire(ctx)
	if err != nil {
		h.safeLogger().Warn("Failed to get settings", slog.Any("error", err))

		sharedserver.RespondError(c, http.StatusInternalServerError, "Failed to get settings", nil)

		return
	}

	defer release()

	s := h.Settings.Get()

	if err := ctx.Err(); err != nil {
		h.safeLogger().Warn("Failed to get settings", slog.Any("error", err))

		sharedserver.RespondError(c, http.StatusInternalServerError, "Failed to get settings", nil)

		return
	}

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

	ctx, cancel := context.WithTimeout(c.Request.Context(), constants.RequestTimeout.AdminRequest)

	defer cancel()

	release, err := h.settingsOperationGate().acquire(ctx)
	if err != nil {
		h.safeLogger().Warn("Failed to update settings", slog.Any("error", err))

		sharedserver.RespondError(c, http.StatusInternalServerError, "Failed to update settings", nil)

		return
	}

	defer release()

	current := h.Settings.Get()

	if err := ctx.Err(); err != nil {
		h.safeLogger().Warn("Failed to update settings", slog.Any("error", err))

		sharedserver.RespondError(c, http.StatusInternalServerError, "Failed to update settings", nil)

		return
	}

	alarmAdvanceUpdated := req.applyTo(&current)

	if alarmAdvanceUpdated {
		if err := h.Settings.Update(current); err != nil {
			h.safeLogger().Error("Failed to update settings", slog.Any("error", err))

			sharedserver.RespondError(c, 500, "Failed to update settings", nil)

			return
		}
	}

	runtime := h.applySettingsRuntime(ctx, current, alarmAdvanceUpdated)

	if alarmAdvanceUpdated {
		h.logSettingsUpdate(current, runtime)
	}

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

		current.TargetMinutes = targetpolicy.BuildRuntimeTargetMinutes(*req.AlarmAdvanceMinutes)
	}

	return alarmAdvanceUpdated
}

func (h *SettingsHandler) applySettingsRuntime(ctx context.Context, current settingssvc.Settings, alarmAdvanceUpdated bool) map[string]any {
	runtime := map[string]any{}

	if alarmAdvanceUpdated {
		alarmAdvanceResult, err := h.ApplyAlarmAdvanceMinutes(ctx, current.AlarmAdvanceMinutes)
		if err != nil {
			h.safeLogger().Warn("알람 사전 알림 시점 적용 확인 실패", slog.Int("minutes", current.AlarmAdvanceMinutes), slog.Any("error", err))
		}

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
