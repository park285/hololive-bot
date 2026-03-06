package app

import (
	"github.com/gin-gonic/gin"

	"github.com/kapu/hololive-kakao-bot-go/internal/server"
)

func registerAlarmRoutes(holoAPI *gin.RouterGroup, handler *server.AlarmAPIHandler) {
	holoAPI.GET("/alarms", handler.GetAlarms)
	holoAPI.DELETE("/alarms", handler.DeleteAlarm)
}

func registerMemberRoutes(holoAPI *gin.RouterGroup, handler *server.MemberAPIHandler) {
	holoAPI.GET("/members", handler.GetMembers)
	holoAPI.POST("/members", handler.AddMember)
	holoAPI.POST("/members/:id/aliases", handler.AddAlias)
	holoAPI.DELETE("/members/:id/aliases", handler.RemoveAlias)
	holoAPI.PATCH("/members/:id/graduation", handler.SetGraduation)
	holoAPI.PATCH("/members/:id/channel", handler.UpdateChannelID)
	holoAPI.PATCH("/members/:id/name", handler.UpdateMemberName)
}

func registerRoomRoutes(holoAPI *gin.RouterGroup, handler *server.RoomAPIHandler) {
	holoAPI.GET("/rooms", handler.GetRooms)
	holoAPI.POST("/rooms", handler.AddRoom)
	holoAPI.DELETE("/rooms", handler.RemoveRoom)
	holoAPI.POST("/rooms/acl", handler.SetACL)
}

func registerMajorEventRoutes(holoAPI *gin.RouterGroup, handler *server.MajorEventAPIHandler) {
	holoAPI.POST("/majorevent/trigger", handler.TriggerMajorEventNotification)
	holoAPI.POST("/majorevent/monthly-trigger", handler.TriggerMajorEventMonthlyNotification)
}

func registerMilestoneRoutes(holoAPI *gin.RouterGroup, handler *server.MilestoneAPIHandler) {
	holoAPI.GET("/milestones", handler.GetMilestones)
	holoAPI.GET("/milestones/near", handler.GetNearMilestoneMembers)
	holoAPI.GET("/milestones/stats", handler.GetMilestoneStats)
}

func registerProfileRoutes(holoAPI *gin.RouterGroup, handler *server.ProfileAPIHandler) {
	holoAPI.GET("/profiles", handler.GetProfile)
	holoAPI.GET("/profiles/name", handler.GetProfileByName)
}

func registerSettingsRoutes(holoAPI *gin.RouterGroup, handler *server.SettingsAPIHandler) {
	holoAPI.GET("/logs", handler.GetLogs)
	holoAPI.GET("/settings", handler.GetSettings)
	holoAPI.POST("/settings", handler.UpdateSettings)
	holoAPI.POST("/settings/llm", handler.UpdateLLMSettings)
	holoAPI.POST("/names/room", handler.SetRoomName)
	holoAPI.POST("/names/user", handler.SetUserName)
}

func registerStatsRoutes(holoAPI *gin.RouterGroup, statsHandler *server.StatsAPIHandler, streamHandler *server.StreamAPIHandler) {
	holoAPI.GET("/stats", statsHandler.GetStats)
	holoAPI.GET("/stats/channels", streamHandler.GetChannelStats)
	holoAPI.GET("/streams/live", streamHandler.GetLiveStreams)
	holoAPI.GET("/streams/upcoming", streamHandler.GetUpcomingStreams)
	holoAPI.GET("/channels", streamHandler.GetChannel)
	holoAPI.GET("/channels/search", streamHandler.SearchChannels)
}

func registerTemplateRoutes(holoAPI *gin.RouterGroup, handler *server.TemplateAPIHandler) {
	holoAPI.GET("/templates", handler.GetTemplates)
	holoAPI.GET("/templates/:key", handler.GetTemplateByKey)
	holoAPI.PUT("/templates/:key", handler.UpsertTemplate)
	holoAPI.DELETE("/templates/:key", handler.DeleteTemplateOverride)
	holoAPI.POST("/templates/:key/preview", handler.PreviewTemplate)
	holoAPI.GET("/templates/:key/revisions", handler.GetTemplateRevisions)
	holoAPI.GET("/templates/:key/revisions/:id", handler.GetTemplateRevision)
}
