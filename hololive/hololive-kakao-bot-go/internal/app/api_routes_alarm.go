package app

import (
	"github.com/gin-gonic/gin"

	"github.com/kapu/hololive-kakao-bot-go/internal/server"
)

func registerAlarmRoutes(holoAPI *gin.RouterGroup, handler *server.AlarmAPIHandler) {
	holoAPI.GET("/alarms", handler.GetAlarms)
	holoAPI.DELETE("/alarms", handler.DeleteAlarm)
}
