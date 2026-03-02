package server

import (
	"github.com/gin-gonic/gin"
	sharedserver "github.com/kapu/hololive-shared/pkg/server"
)

func (h *SettingsAPIHandler) sharedSettingsHandler() *sharedserver.SettingsHandler {
	return &sharedserver.SettingsHandler{
		Logger:   h.logger,
		Alarm:    h.alarm,
		Activity: h.activity,
		ReadRecentLogs: func(limit int) (any, error) {
			return h.activity.GetRecentLogs(limit)
		},
		Settings:        h.settings,
		SettingsApplier: h.settingsApplier,
	}
}

// SetRoomName: 방 ID에 대한 표시 이름을 설정합니다.
func (h *SettingsAPIHandler) SetRoomName(c *gin.Context) {
	h.sharedSettingsHandler().SetRoomName(c)
}

// SetUserName: 사용자 ID에 대한 표시 이름을 설정합니다.
func (h *SettingsAPIHandler) SetUserName(c *gin.Context) {
	h.sharedSettingsHandler().SetUserName(c)
}

// GetLogs: 활동 로그를 반환합니다.
func (h *SettingsAPIHandler) GetLogs(c *gin.Context) {
	h.sharedSettingsHandler().GetLogs(c)
}

// GetSettings: 현재 설정을 반환합니다.
func (h *SettingsAPIHandler) GetSettings(c *gin.Context) {
	h.sharedSettingsHandler().GetSettings(c)
}

// UpdateSettings: 설정을 업데이트합니다.
func (h *SettingsAPIHandler) UpdateSettings(c *gin.Context) {
	h.sharedSettingsHandler().UpdateSettings(c)
}

// UpdateLLMSettings: llm-scheduler 런타임 설정/실행 트리거를 업데이트합니다.
func (h *SettingsAPIHandler) UpdateLLMSettings(c *gin.Context) {
	h.sharedSettingsHandler().UpdateLLMSettings(c)
}
