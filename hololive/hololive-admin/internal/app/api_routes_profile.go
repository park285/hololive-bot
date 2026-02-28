package app

import (
	"github.com/gin-gonic/gin"

	"github.com/kapu/hololive-admin/internal/server"
)

func registerProfileRoutes(holoAPI *gin.RouterGroup, handler *server.ProfileAPIHandler) {
	// 프로필 API (Tauri 앱 전용)
	holoAPI.GET("/profiles", handler.GetProfile)
	holoAPI.GET("/profiles/name", handler.GetProfileByName)
}
