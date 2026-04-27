package server

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/domain"
	sharedserver "github.com/kapu/hololive-shared/pkg/server"
	sharedsettings "github.com/kapu/hololive-shared/pkg/server/settings"
	sharedchecker "github.com/kapu/hololive-shared/pkg/service/alarm/checker"
	settingssvc "github.com/kapu/hololive-shared/pkg/service/settings"
)

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

const (
	minAlarmAdvanceMinutes = 0
	maxAlarmAdvanceMinutes = 24 * 60
)

func (h *SettingsHandler) safeLogger() *slog.Logger {
	if h != nil && h.Logger != nil {
		return h.Logger
	}

	return slog.Default()
}

func (h *SettingsHandler) logActivity(entryType, summary string, details map[string]any) {
	if h != nil && h.Activity != nil {
		h.Activity.Log(entryType, summary, details)
	}
}

func (h *SettingsHandler) requireAlarm(c *gin.Context) bool {
	if h == nil || h.Alarm == nil {
		sharedserver.RespondError(c, http.StatusServiceUnavailable, "alarm service not available", nil)
		return false
	}

	return true
}

func (h *SettingsHandler) requireSettings(c *gin.Context) bool {
	if h == nil || h.Settings == nil {
		sharedserver.RespondError(c, http.StatusServiceUnavailable, "settings service not available", nil)
		return false
	}

	return true
}

func (h *SettingsHandler) requireApplier(c *gin.Context) bool {
	if h == nil || h.SettingsApplier == nil {
		sharedserver.RespondError(c, http.StatusServiceUnavailable, "settings applier not available", nil)
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

	c.JSON(200, gin.H{
		"status":  "ok",
		"message": "Room name set successfully",
	})
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

	c.JSON(200, gin.H{
		"status":  "ok",
		"message": "User name set successfully",
	})
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
	c.JSON(200, gin.H{"status": "ok", "logs": logs})
}

func (h *SettingsHandler) GetSettings(c *gin.Context) {
	if !h.requireSettings(c) || !h.requireApplier(c) {
		return
	}

	s := h.Settings.Get()
	runtime := h.ScraperProxyRuntimeState(s.ScraperProxyEnabled).AsMap()
	c.JSON(200, gin.H{"status": "ok", "settings": s, "runtime": runtime})
}

func (h *SettingsHandler) UpdateSettings(c *gin.Context) {
	var req struct {
		AlarmAdvanceMinutes *int  `json:"alarmAdvanceMinutes"`
		ScraperProxyEnabled *bool `json:"scraperProxyEnabled"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		h.safeLogger().Warn("Invalid request body", slog.Any("error", err))
		sharedserver.RespondError(c, 400, "invalid request body", nil)
		return
	}

	if !h.requireSettings(c) || !h.requireApplier(c) {
		return
	}

	if req.AlarmAdvanceMinutes != nil {
		minutes := *req.AlarmAdvanceMinutes
		if minutes < minAlarmAdvanceMinutes || minutes > maxAlarmAdvanceMinutes {
			sharedserver.RespondError(
				c,
				400,
				fmt.Sprintf("alarmAdvanceMinutes must be between %d and %d", minAlarmAdvanceMinutes, maxAlarmAdvanceMinutes),
				nil,
			)
			return
		}
	}

	current := h.Settings.Get()
	alarmAdvanceUpdated := false
	if req.AlarmAdvanceMinutes != nil {
		current.AlarmAdvanceMinutes = *req.AlarmAdvanceMinutes
		current.TargetMinutes = sharedchecker.BuildRuntimeTargetMinutes(*req.AlarmAdvanceMinutes)
		alarmAdvanceUpdated = true
	}
	if req.ScraperProxyEnabled != nil {
		current.ScraperProxyEnabled = *req.ScraperProxyEnabled
	}

	if err := h.Settings.Update(current); err != nil {
		h.safeLogger().Error("Failed to update settings", slog.Any("error", err))
		sharedserver.RespondError(c, 500, "Failed to update settings", nil)
		return
	}

	runtime := h.ApplyScraperProxy(c.Request.Context(), current.ScraperProxyEnabled).AsMap()
	if alarmAdvanceUpdated {
		maps.Copy(runtime, h.ApplyAlarmAdvanceMinutes(c.Request.Context(), current.AlarmAdvanceMinutes).AsMap())
	}
	h.publishUpdateResult(c.Request.Context(), runtime, req.ScraperProxyEnabled, req.AlarmAdvanceMinutes)

	h.logActivity("settings_update", "Settings updated", map[string]any{
		"alarm_advance_minutes":  current.AlarmAdvanceMinutes,
		"scraper_proxy_enabled":  current.ScraperProxyEnabled,
		"scraper_runtime_status": runtime,
	})

	c.JSON(200, gin.H{"status": "ok", "message": "Settings updated", "settings": current, "runtime": runtime})
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
	var req struct {
		MajorEventScrapeHourKST *int  `json:"majorEventScrapeHourKST"`
		MajorEventScrapeRunNow  *bool `json:"majorEventScrapeRunNow"`
		MemberNewsWeeklyRunNow  *bool `json:"memberNewsWeeklyRunNow"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		h.safeLogger().Warn("Invalid request body", slog.Any("error", err))
		sharedserver.RespondError(c, 400, "invalid request body", nil)
		return
	}

	if !h.requireApplier(c) {
		return
	}

	if req.MajorEventScrapeHourKST != nil || req.MajorEventScrapeRunNow != nil {
		sharedserver.RespondError(c, 410, "majorEventScrape* controls are no longer supported; major event scraping is owned by llm-scheduler", nil)
		return
	}
	if req.MemberNewsWeeklyRunNow == nil {
		sharedserver.RespondError(c, 400, "at least one llm setting field is required", nil)
		return
	}
	if req.MemberNewsWeeklyRunNow != nil && !*req.MemberNewsWeeklyRunNow {
		sharedserver.RespondError(c, 400, "memberNewsWeeklyRunNow must be true when provided", nil)
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), constants.RequestTimeout.AdminRequest)
	defer cancel()

	runtime := map[string]any{}
	if req.MemberNewsWeeklyRunNow != nil && *req.MemberNewsWeeklyRunNow {
		runtime["membernews_weekly_run_now"] = h.ApplyMemberNewsWeeklyRunNow(ctx).AsMap()
	}

	h.logActivity("llm_settings_update", "LLM settings updated", map[string]any{
		"membernews_weekly_run_now": req.MemberNewsWeeklyRunNow,
		"runtime":                   runtime,
	})

	c.JSON(200, gin.H{
		"status":  "ok",
		"message": "LLM settings updated",
		"runtime": runtime,
	})
}
