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

package settings

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/gin-gonic/gin"

	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/domain"
	settingssvc "github.com/kapu/hololive-shared/pkg/service/settings"
)

// SettingsActivityLogger는 설정 변경 이벤트를 기록하는 최소 인터페이스입니다.
type SettingsActivityLogger interface {
	Log(entryType, summary string, details map[string]any)
}

// SettingsReadRecentLogsFunc는 최근 활동 로그 조회 함수 시그니처입니다.
type SettingsReadRecentLogsFunc func(limit int) (any, error)

// SettingsHandler는 설정 관련 공통 HTTP 핸들러 로직을 제공합니다.
type SettingsHandler struct {
	Logger          *slog.Logger
	Alarm           domain.AlarmCRUD
	Activity        SettingsActivityLogger
	ReadRecentLogs  SettingsReadRecentLogsFunc
	Settings        settingssvc.ReadWriter
	ConfigPublisher ConfigPublisher
	SettingsApplier
}

// SetRoomName: 방 ID에 대한 표시 이름을 설정합니다.
func (h *SettingsHandler) SetRoomName(c *gin.Context) {
	var req struct {
		RoomID   string `json:"roomId" binding:"required"`
		RoomName string `json:"roomName" binding:"required,min=1"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		h.Logger.Warn("Invalid request body", slog.Any("error", err))
		c.JSON(400, gin.H{"error": "invalid request body"})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), constants.RequestTimeout.AdminRequest)
	defer cancel()

	if err := h.Alarm.SetRoomName(ctx, req.RoomID, req.RoomName); err != nil {
		h.Logger.Error("Failed to set room name", slog.Any("error", err))
		c.JSON(500, gin.H{"error": "Failed to set room name"})
		return
	}

	h.Logger.Info("Room name set",
		slog.String("room_id", req.RoomID),
		slog.String("room_name", req.RoomName),
	)

	h.Activity.Log("name_update", fmt.Sprintf("Room name set: %s -> %s", req.RoomID, req.RoomName), map[string]any{
		"room_id":   req.RoomID,
		"room_name": req.RoomName,
	})

	c.JSON(200, gin.H{
		"status":  "ok",
		"message": "Room name set successfully",
	})
}

// SetUserName: 사용자 ID에 대한 표시 이름을 설정합니다.
func (h *SettingsHandler) SetUserName(c *gin.Context) {
	var req struct {
		UserID   string `json:"userId" binding:"required"`
		UserName string `json:"userName" binding:"required,min=1"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		h.Logger.Warn("Invalid request body", slog.Any("error", err))
		c.JSON(400, gin.H{"error": "invalid request body"})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), constants.RequestTimeout.AdminRequest)
	defer cancel()

	if err := h.Alarm.SetUserName(ctx, req.UserID, req.UserName); err != nil {
		h.Logger.Error("Failed to set user name", slog.Any("error", err))
		c.JSON(500, gin.H{"error": "Failed to set user name"})
		return
	}

	h.Logger.Info("User name set",
		slog.String("user_id", req.UserID),
		slog.String("user_name", req.UserName),
	)

	c.JSON(200, gin.H{
		"status":  "ok",
		"message": "User name set successfully",
	})
}

// GetLogs: 활동 로그를 반환합니다.
func (h *SettingsHandler) GetLogs(c *gin.Context) {
	logs, err := h.ReadRecentLogs(100)
	if err != nil {
		h.Logger.Error("Failed to get logs", slog.Any("error", err))
		c.JSON(500, gin.H{"error": "Failed to get logs"})
		return
	}
	c.JSON(200, gin.H{"status": "ok", "logs": logs})
}

// GetSettings: 현재 설정을 반환합니다.
func (h *SettingsHandler) GetSettings(c *gin.Context) {
	s := h.Settings.Get()
	runtime := h.ScraperProxyRuntimeState(s.ScraperProxyEnabled).AsMap()
	c.JSON(200, gin.H{"status": "ok", "settings": s, "runtime": runtime})
}

// UpdateSettings: 설정을 업데이트합니다.
func (h *SettingsHandler) UpdateSettings(c *gin.Context) {
	var req struct {
		AlarmAdvanceMinutes *int  `json:"alarmAdvanceMinutes"`
		ScraperProxyEnabled *bool `json:"scraperProxyEnabled"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		h.Logger.Warn("Invalid request body", slog.Any("error", err))
		c.JSON(400, gin.H{"error": "invalid request body"})
		return
	}

	current := h.Settings.Get()
	alarmAdvanceUpdated := false
	if req.AlarmAdvanceMinutes != nil {
		current.AlarmAdvanceMinutes = *req.AlarmAdvanceMinutes
		alarmAdvanceUpdated = true
	}
	if req.ScraperProxyEnabled != nil {
		current.ScraperProxyEnabled = *req.ScraperProxyEnabled
	}

	if err := h.Settings.Update(current); err != nil {
		h.Logger.Error("Failed to update settings", slog.Any("error", err))
		c.JSON(500, gin.H{"error": "Failed to update settings"})
		return
	}

	runtime := h.ApplyScraperProxy(c.Request.Context(), current.ScraperProxyEnabled).AsMap()
	if alarmAdvanceUpdated {
		for k, v := range h.ApplyAlarmAdvanceMinutes(c.Request.Context(), current.AlarmAdvanceMinutes).AsMap() {
			runtime[k] = v
		}
	}
	h.publishUpdateResult(c.Request.Context(), runtime, req.ScraperProxyEnabled, req.AlarmAdvanceMinutes)

	h.Activity.Log("settings_update", "Settings updated", map[string]any{
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
			runtime["config_publish_scraper_proxy_error"] = err.Error()
			h.Logger.Warn("Failed to publish scraper proxy update", slog.Any("error", err))
		} else {
			runtime["config_publish_scraper_proxy"] = true
		}
	}

	if alarmAdvanceMinutes != nil {
		if err := h.ConfigPublisher.PublishAlarmAdvanceMinutes(ctx, *alarmAdvanceMinutes); err != nil {
			runtime["config_publish_alarm_advance_minutes"] = false
			runtime["config_publish_alarm_advance_minutes_error"] = err.Error()
			h.Logger.Warn("Failed to publish alarm advance minutes update", slog.Any("error", err))
		} else {
			runtime["config_publish_alarm_advance_minutes"] = true
		}
	}
}

// UpdateLLMSettings: llm-scheduler 런타임 설정/실행 트리거를 업데이트합니다.
func (h *SettingsHandler) UpdateLLMSettings(c *gin.Context) {
	var req struct {
		MajorEventScrapeHourKST *int  `json:"majorEventScrapeHourKST"`
		MajorEventScrapeRunNow  *bool `json:"majorEventScrapeRunNow"`
		MemberNewsWeeklyRunNow  *bool `json:"memberNewsWeeklyRunNow"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		h.Logger.Warn("Invalid request body", slog.Any("error", err))
		c.JSON(400, gin.H{"error": "invalid request body"})
		return
	}

	if req.MajorEventScrapeHourKST != nil || req.MajorEventScrapeRunNow != nil {
		c.JSON(410, gin.H{"error": "majorEventScrape* controls are no longer supported; major event scraping is owned by llm-scheduler"})
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
		runtime["membernews_weekly_run_now"] = h.ApplyMemberNewsWeeklyRunNow(ctx).AsMap()
	}

	h.Activity.Log("llm_settings_update", "LLM settings updated", map[string]any{
		"membernews_weekly_run_now": req.MemberNewsWeeklyRunNow,
		"runtime":                   runtime,
	})

	c.JSON(200, gin.H{
		"status":  "ok",
		"message": "LLM settings updated",
		"runtime": runtime,
	})
}
