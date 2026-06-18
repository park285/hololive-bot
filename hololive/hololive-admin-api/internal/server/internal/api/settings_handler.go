package api

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/kapu/hololive-shared/pkg/constants"
	contractssettings "github.com/kapu/hololive-shared/pkg/contracts/settings"
	"github.com/kapu/hololive-shared/pkg/domain"
	sharedserver "github.com/kapu/hololive-shared/pkg/server"
	sharedsettings "github.com/kapu/hololive-shared/pkg/server/settings"
	sharedchecker "github.com/kapu/hololive-shared/pkg/service/alarm/checker"
	settingssvc "github.com/kapu/hololive-shared/pkg/service/settings"
	"github.com/park285/shared-go/pkg/ginjson"
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

type ConfigPublisher interface {
	PublishScraperProxy(ctx context.Context, enabled bool) error
	PublishAlarmAdvanceMinutes(ctx context.Context, minutes int) error
}

type SettingsHandler struct {
	Logger          *slog.Logger
	Alarm           domain.AlarmCRUD
	Activity        SettingsActivityLogger
	ReadRecentLogs  SettingsReadRecentLogsFunc
	Settings        settingssvc.ReadWriter
	ConfigPublisher ConfigPublisher
	sharedsettings.SettingsApplier
}

type updateSettingsRequest struct {
	AlarmAdvanceMinutes *int  `json:"alarmAdvanceMinutes"`
	ScraperProxyEnabled *bool `json:"scraperProxyEnabled"`
}

type updateLLMSettingsRequest struct {
	MajorEventScrapeHourKST *int  `json:"majorEventScrapeHourKST"`
	MajorEventScrapeRunNow  *bool `json:"majorEventScrapeRunNow"`
	MemberNewsWeeklyRunNow  *bool `json:"memberNewsWeeklyRunNow"`
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

func (h *SettingsHandler) SetRoomName(c *gin.Context) {
	var req struct {
		RoomID   string `json:"roomId" binding:"required"`
		RoomName string `json:"roomName" binding:"required,min=1"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		h.safeLogger().Warn("Invalid request body", slog.Any("error", err))
		sharedserver.RespondError(c, 400, "invalid request body", nil)
		return
	}

	if !h.requireAlarm(c) {
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), constants.RequestTimeout.AdminRequest)
	defer cancel()

	if err := h.Alarm.SetRoomName(ctx, req.RoomID, req.RoomName); err != nil {
		h.safeLogger().Error("Failed to set room name", slog.Any("error", err))
		sharedserver.RespondError(c, 500, "Failed to set room name", nil)
		return
	}

	h.safeLogger().Info("Room name set",
		slog.String("room_id", req.RoomID),
		slog.String("room_name", req.RoomName),
	)

	h.logActivity("name_update", fmt.Sprintf("Room name set: %s -> %s", req.RoomID, req.RoomName), map[string]any{
		"room_id":   req.RoomID,
		"room_name": req.RoomName,
	})

	ginjson.Respond(c, 200, statusMessageResponse{Status: "ok", Message: "Room name set successfully"})
}

func (h *SettingsHandler) SetUserName(c *gin.Context) {
	var req struct {
		UserID   string `json:"userId" binding:"required"`
		UserName string `json:"userName" binding:"required,min=1"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		h.safeLogger().Warn("Invalid request body", slog.Any("error", err))
		sharedserver.RespondError(c, 400, "invalid request body", nil)
		return
	}

	if !h.requireAlarm(c) {
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), constants.RequestTimeout.AdminRequest)
	defer cancel()

	if err := h.Alarm.SetUserName(ctx, req.UserID, req.UserName); err != nil {
		h.safeLogger().Error("Failed to set user name", slog.Any("error", err))
		sharedserver.RespondError(c, 500, "Failed to set user name", nil)
		return
	}

	h.safeLogger().Info("User name set",
		slog.String("user_id", req.UserID),
		slog.String("user_name", req.UserName),
	)

	ginjson.Respond(c, 200, statusMessageResponse{Status: "ok", Message: "User name set successfully"})
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
	runtimeState := h.ScraperProxyRuntimeState(s.ScraperProxyEnabled)
	runtime := runtimeState.AsMap()
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
	h.publishUpdateResult(c.Request.Context(), runtime, req.ScraperProxyEnabled, req.AlarmAdvanceMinutes)
	h.logSettingsUpdate(current, runtime)

	ginjson.Respond(c, 200, settingsUpdateResponse{Status: "ok", Message: "Settings updated", Settings: current, Runtime: runtime})
}

func (h *SettingsHandler) bindUpdateSettingsRequest(c *gin.Context) (updateSettingsRequest, bool) {
	var req updateSettingsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
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
	if req.ScraperProxyEnabled != nil {
		current.ScraperProxyEnabled = *req.ScraperProxyEnabled
	}
	return alarmAdvanceUpdated
}

func (h *SettingsHandler) applySettingsRuntime(ctx context.Context, current settingssvc.Settings, alarmAdvanceUpdated bool) map[string]any {
	scraperProxyResult := h.ApplyScraperProxy(ctx, current.ScraperProxyEnabled)
	runtime := scraperProxyResult.AsMap()
	if alarmAdvanceUpdated {
		alarmAdvanceResult := h.ApplyAlarmAdvanceMinutes(ctx, current.AlarmAdvanceMinutes)
		maps.Copy(runtime, alarmAdvanceResult.AsMap())
	}
	return runtime
}

func (h *SettingsHandler) logSettingsUpdate(current settingssvc.Settings, runtime map[string]any) {
	h.logActivity("settings_update", "Settings updated", map[string]any{
		"alarm_advance_minutes":  current.AlarmAdvanceMinutes,
		"scraper_proxy_enabled":  current.ScraperProxyEnabled,
		"scraper_runtime_status": runtime,
	})
}

func (h *SettingsHandler) publishUpdateResult(ctx context.Context, runtime map[string]any, scraperProxyEnabled *bool, alarmAdvanceMinutes *int) {
	if h.ConfigPublisher == nil {
		return
	}

	if scraperProxyEnabled != nil {
		if err := h.ConfigPublisher.PublishScraperProxy(ctx, *scraperProxyEnabled); err != nil {
			runtime["config_publish_scraper_proxy"] = false
			runtime["config_publish_scraper_proxy_error"] = fmt.Sprint(err)
			h.safeLogger().Warn("Failed to publish scraper proxy update", slog.Any("error", err))
		} else {
			runtime["config_publish_scraper_proxy"] = true
		}
	}

	if alarmAdvanceMinutes != nil {
		if err := h.ConfigPublisher.PublishAlarmAdvanceMinutes(ctx, *alarmAdvanceMinutes); err != nil {
			runtime["config_publish_alarm_advance_minutes"] = false
			runtime["config_publish_alarm_advance_minutes_error"] = fmt.Sprint(err)
			h.safeLogger().Warn("Failed to publish alarm advance minutes update", slog.Any("error", err))
		} else {
			runtime["config_publish_alarm_advance_minutes"] = true
		}
	}
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
	if err := c.ShouldBindJSON(&req); err != nil {
		h.safeLogger().Warn("Invalid request body", slog.Any("error", err))
		sharedserver.RespondError(c, 400, "invalid request body", nil)
		return req, false
	}
	return req, true
}

func (req updateLLMSettingsRequest) validate(c *gin.Context) bool {
	if req.MajorEventScrapeHourKST != nil || req.MajorEventScrapeRunNow != nil {
		sharedserver.RespondError(c, 410, "majorEventScrape* controls are no longer supported; major event scraping is owned by llm-scheduler", nil)
		return false
	}
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
