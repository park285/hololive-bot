package app

import (
	"github.com/gin-gonic/gin"

	"github.com/kapu/hololive-admin/internal/server"
)

func registerSettingsRoutes(holoAPI *gin.RouterGroup, handler *server.SettingsAPIHandler) {
	holoAPI.GET("/logs", handler.GetLogs)
	holoAPI.GET("/settings", handler.GetSettings)
	holoAPI.POST("/settings", handler.UpdateSettings)
	holoAPI.POST("/settings/llm", handler.UpdateLLMSettings)
	holoAPI.POST("/names/room", handler.SetRoomName)
	holoAPI.POST("/names/user", handler.SetUserName)
}
