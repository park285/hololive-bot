package app

import (
	"github.com/gin-gonic/gin"

	"github.com/kapu/hololive-kakao-bot-go/internal/server"
)

func registerMajorEventRoutes(holoAPI *gin.RouterGroup, handler *server.MajorEventAPIHandler) {
	// 이벤트 알림 수동 트리거 (테스트용)
	holoAPI.POST("/majorevent/trigger", handler.TriggerMajorEventNotification)
	holoAPI.POST("/majorevent/monthly-trigger", handler.TriggerMajorEventMonthlyNotification)
}
