package app

import (
	"github.com/gin-gonic/gin"

	"github.com/kapu/hololive-kakao-bot-go/internal/server"
)

func registerMilestoneRoutes(holoAPI *gin.RouterGroup, handler *server.MilestoneAPIHandler) {
	// 마일스톤 API
	holoAPI.GET("/milestones", handler.GetMilestones)
	holoAPI.GET("/milestones/near", handler.GetNearMilestoneMembers)
	holoAPI.GET("/milestones/stats", handler.GetMilestoneStats)
}
