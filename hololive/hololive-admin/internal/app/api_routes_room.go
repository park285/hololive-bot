package app

import (
	"github.com/gin-gonic/gin"

	"github.com/kapu/hololive-admin/internal/server"
)

func registerRoomRoutes(holoAPI *gin.RouterGroup, handler *server.RoomAPIHandler) {
	holoAPI.GET("/rooms", handler.GetRooms)
	holoAPI.POST("/rooms", handler.AddRoom)
	holoAPI.DELETE("/rooms", handler.RemoveRoom)
	holoAPI.POST("/rooms/acl", handler.SetACL)
}
