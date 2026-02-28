package server

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/gin-gonic/gin"

	"github.com/kapu/hololive-shared/pkg/constants"
)

// SetRoomName: 방 ID에 대한 표시 이름을 설정합니다.
func (h *SettingsAPIHandler) SetRoomName(c *gin.Context) {
	var req struct {
		RoomID   string `json:"roomId" binding:"required"`
		RoomName string `json:"roomName" binding:"required,min=1"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		h.logger.Warn("Invalid request body", slog.Any("error", err))
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), constants.RequestTimeout.AdminRequest)
	defer cancel()

	// AlarmService를 통해 Valkey에 저장함
	if err := h.alarm.SetRoomName(ctx, req.RoomID, req.RoomName); err != nil {
		h.logger.Error("Failed to set room name", slog.Any("error", err))
		c.JSON(500, gin.H{"error": "Failed to set room name"})
		return
	}

	h.logger.Info("Room name set",
		slog.String("room_id", req.RoomID),
		slog.String("room_name", req.RoomName),
	)

	h.activity.Log("name_update", fmt.Sprintf("Room name set: %s -> %s", req.RoomID, req.RoomName), map[string]any{
		"room_id":   req.RoomID,
		"room_name": req.RoomName,
	})

	c.JSON(200, gin.H{
		"status":  "ok",
		"message": "Room name set successfully",
	})
}

// SetUserName: 사용자 ID에 대한 표시 이름을 설정합니다.
func (h *SettingsAPIHandler) SetUserName(c *gin.Context) {
	var req struct {
		UserID   string `json:"userId" binding:"required"`
		UserName string `json:"userName" binding:"required,min=1"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		h.logger.Warn("Invalid request body", slog.Any("error", err))
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), constants.RequestTimeout.AdminRequest)
	defer cancel()

	// AlarmService를 통해 Valkey에 저장함
	if err := h.alarm.SetUserName(ctx, req.UserID, req.UserName); err != nil {
		h.logger.Error("Failed to set user name", slog.Any("error", err))
		c.JSON(500, gin.H{"error": "Failed to set user name"})
		return
	}

	h.logger.Info("User name set",
		slog.String("user_id", req.UserID),
		slog.String("user_name", req.UserName),
	)

	c.JSON(200, gin.H{
		"status":  "ok",
		"message": "User name set successfully",
	})
}

// GetLogs: 활동 로그를 반환합니다.
func (h *SettingsAPIHandler) GetLogs(c *gin.Context) {
	logs, err := h.activity.GetRecentLogs(100)
	if err != nil {
		h.logger.Error("Failed to get logs", slog.Any("error", err))
		c.JSON(500, gin.H{"error": "Failed to get logs"})
		return
	}
	c.JSON(200, gin.H{"status": "ok", "logs": logs})
}

// GetSettings: 현재 설정을 반환합니다.
func (h *SettingsAPIHandler) GetSettings(c *gin.Context) {
	s := h.settings.Get()
	runtime := h.settingsApplier.ScraperProxyRuntimeState(s.ScraperProxyEnabled)
	c.JSON(200, gin.H{"status": "ok", "settings": s, "runtime": runtime})
}

// UpdateSettings: 설정을 업데이트합니다.
func (h *SettingsAPIHandler) UpdateSettings(c *gin.Context) {
	var req struct {
		AlarmAdvanceMinutes *int  `json:"alarmAdvanceMinutes"`
		ScraperProxyEnabled *bool `json:"scraperProxyEnabled"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	current := h.settings.Get()
	alarmAdvanceUpdated := false
	if req.AlarmAdvanceMinutes != nil {
		current.AlarmAdvanceMinutes = *req.AlarmAdvanceMinutes
		alarmAdvanceUpdated = true
	}
	if req.ScraperProxyEnabled != nil {
		current.ScraperProxyEnabled = *req.ScraperProxyEnabled
	}

	if err := h.settings.Update(current); err != nil {
		h.logger.Error("Failed to update settings", slog.Any("error", err))
		c.JSON(500, gin.H{"error": "Failed to update settings"})
		return
	}

	runtime := h.settingsApplier.ApplyScraperProxy(c.Request.Context(), current.ScraperProxyEnabled)
	if alarmAdvanceUpdated {
		for k, v := range h.settingsApplier.ApplyAlarmAdvanceMinutes(c.Request.Context(), current.AlarmAdvanceMinutes) {
			runtime[k] = v
		}
	}

	h.activity.Log("settings_update", "Settings updated", map[string]any{
		"alarm_advance_minutes":  current.AlarmAdvanceMinutes,
		"scraper_proxy_enabled":  current.ScraperProxyEnabled,
		"scraper_runtime_status": runtime,
	})

	c.JSON(200, gin.H{"status": "ok", "message": "Settings updated", "settings": current, "runtime": runtime})
}

// UpdateLLMSettings: llm-scheduler 런타임 설정/실행 트리거를 업데이트합니다.
func (h *SettingsAPIHandler) UpdateLLMSettings(c *gin.Context) {
	var req struct {
		// deprecated: major event scraping ownership moved to hololive-scraper-rs
		MajorEventScrapeHourKST *int  `json:"majorEventScrapeHourKST"`
		MajorEventScrapeRunNow  *bool `json:"majorEventScrapeRunNow"`
		MemberNewsWeeklyRunNow  *bool `json:"memberNewsWeeklyRunNow"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	if req.MajorEventScrapeHourKST != nil || req.MajorEventScrapeRunNow != nil {
		c.JSON(410, gin.H{"error": "majorEventScrape* controls are no longer supported; major event scraping is owned by hololive-scraper-rs"})
		return
	}
	if req.MemberNewsWeeklyRunNow == nil {
		c.JSON(400, gin.H{"error": "at least one llm setting field is required"})
		return
	}
	if req.MemberNewsWeeklyRunNow != nil && !*req.MemberNewsWeeklyRunNow {
		c.JSON(400, gin.H{"error": "memberNewsWeeklyRunNow must be true when provided"})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), constants.RequestTimeout.AdminRequest)
	defer cancel()

	runtime := map[string]any{}
	if req.MemberNewsWeeklyRunNow != nil && *req.MemberNewsWeeklyRunNow {
		runtime["membernews_weekly_run_now"] = h.settingsApplier.ApplyMemberNewsWeeklyRunNow(ctx)
	}

	h.activity.Log("llm_settings_update", "LLM settings updated", map[string]any{
		"membernews_weekly_run_now": req.MemberNewsWeeklyRunNow,
		"runtime":                   runtime,
	})

	c.JSON(200, gin.H{
		"status":  "ok",
		"message": "LLM settings updated",
		"runtime": runtime,
	})
}
