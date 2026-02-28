package app

import (
	"github.com/gin-gonic/gin"

	"github.com/kapu/hololive-admin/internal/server"
)

func registerStatsRoutes(holoAPI *gin.RouterGroup, statsHandler *server.StatsAPIHandler, streamHandler *server.StreamAPIHandler) {
	holoAPI.GET("/stats", statsHandler.GetStats)
	holoAPI.GET("/stats/channels", streamHandler.GetChannelStats)
	holoAPI.GET("/streams/live", streamHandler.GetLiveStreams)
	holoAPI.GET("/streams/upcoming", streamHandler.GetUpcomingStreams)

	// 채널 정보 API (Holodex 기반 - 프로필 이미지 포함)
	holoAPI.GET("/channels", streamHandler.GetChannel)
	holoAPI.GET("/channels/search", streamHandler.SearchChannels)
}
