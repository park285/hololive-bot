package app

import (
	"github.com/gin-gonic/gin"

	"github.com/kapu/hololive-admin/internal/server"
)

func registerMemberRoutes(holoAPI *gin.RouterGroup, handler *server.MemberAPIHandler) {
	holoAPI.GET("/members", handler.GetMembers)
	holoAPI.POST("/members", handler.AddMember)
	holoAPI.POST("/members/:id/aliases", handler.AddAlias)
	holoAPI.DELETE("/members/:id/aliases", handler.RemoveAlias)
	holoAPI.PATCH("/members/:id/graduation", handler.SetGraduation)
	holoAPI.PATCH("/members/:id/channel", handler.UpdateChannelID)
	holoAPI.PATCH("/members/:id/name", handler.UpdateMemberName)
}
