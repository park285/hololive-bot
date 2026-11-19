package httpapi

import (
	"github.com/gin-gonic/gin"

	"github.com/kapu/hololive-api/internal/planes/admin/internal/server/api"
)

func registerAlarmRoutes(holoAPI *gin.RouterGroup, handler *api.AlarmHandler) {
	holoAPI.GET("/alarms", handler.GetAlarms)
	holoAPI.DELETE("/alarms", handler.DeleteAlarm)
	holoAPI.GET("/dispatch/summary", handler.GetDispatchSummary)
	holoAPI.GET("/dispatch/failures", handler.GetDispatchFailures)
	holoAPI.GET("/dispatch/deliveries", handler.GetDispatchDeliveries)
	holoAPI.GET("/dispatch/deliveries/:id", handler.GetDispatchDelivery)
	holoAPI.GET("/dispatch/deliveries/:id/actions", handler.GetDispatchActions)
	holoAPI.POST("/dispatch/deliveries/:id/requeue", handler.RequeueDispatchDelivery)
	holoAPI.POST("/dispatch/deliveries/:id/settle", handler.SettleDispatchDelivery)
}

func registerMemberRoutes(holoAPI *gin.RouterGroup, handler *api.MemberHandler) {
	holoAPI.GET("/members", handler.GetMembers)
	holoAPI.GET("/members/calendar", handler.GetCalendar)
	holoAPI.POST("/members", handler.AddMember)
	holoAPI.POST("/members/:id/aliases", handler.AddAlias)
	holoAPI.DELETE("/members/:id/aliases", handler.RemoveAlias)
	holoAPI.PATCH("/members/:id/graduation", handler.SetGraduation)
	holoAPI.PATCH("/members/:id/channel", handler.UpdateChannelID)
	holoAPI.PATCH("/members/:id/name", handler.UpdateMemberName)
}

func registerRoomRoutes(holoAPI *gin.RouterGroup, handler *api.RoomHandler) {
	holoAPI.GET("/rooms", handler.GetRooms)
	holoAPI.GET("/rooms/joined", handler.GetJoinedRooms)
	holoAPI.POST("/rooms", handler.AddRoom)
	holoAPI.DELETE("/rooms", handler.RemoveRoom)
	holoAPI.POST("/rooms/acl", handler.SetACL)
}

func registerSettingsRoutes(holoAPI *gin.RouterGroup, handler *api.SettingsHandler) {
	holoAPI.GET("/settings", handler.GetSettings)
	holoAPI.POST("/settings", handler.UpdateSettings)
	holoAPI.GET("/x-spaces/session", handler.GetXSpaceSession)
	holoAPI.POST("/x-spaces/session", handler.SubmitXSpaceSession)
	holoAPI.POST("/names/room", handler.SetRoomName)
}

func registerSchedulePosterRoutes(holoAPI *gin.RouterGroup, handler *api.SchedulePosterHandler) {
	holoAPI.GET("/members/:id/schedule-card-template", handler.MemberScheduleCardTemplate)
	holoAPI.PATCH("/members/:id/schedule-card-template", handler.MemberScheduleCardTemplate)
	holoAPI.GET("/schedule-posters/summary", handler.GetSchedulePosterSummary)
	holoAPI.POST("/schedule-posters/collect", handler.CollectSchedulePosters)
	holoAPI.GET("/schedule-posters/memo", handler.GetSchedulePosterMemo)
	holoAPI.POST("/schedule-posters/memo", handler.SaveSchedulePosterMemo)
	holoAPI.GET("/schedule-posters", handler.GetSchedulePosters)
	holoAPI.GET("/schedule-posters/:id", handler.GetSchedulePoster)
	holoAPI.GET("/schedule-posters/:id/image", handler.GetSchedulePosterImage)
	holoAPI.GET("/schedule-posters/:id/changes", handler.GetSchedulePosterChanges)
	holoAPI.POST("/schedule-posters/:id/changes/revoke", handler.RevokeSchedulePosterChange)
	holoAPI.POST("/schedule-posters/:id/changes/restore", handler.RestoreSchedulePosterChange)
	holoAPI.GET("/schedule-posters/:id/rows", handler.GetSchedulePosterRows)
	holoAPI.POST("/schedule-posters/:id/rows", handler.RequestSchedulePosterRows)
	holoAPI.POST("/schedule-posters/:id/rows/polish", handler.PolishSchedulePosterRow)
	holoAPI.POST("/schedule-posters/:id/changes/polish", handler.PolishSchedulePosterAddedSlot)
	holoAPI.POST("/schedule-posters/:id/confirm", handler.ConfirmSchedulePoster)
	holoAPI.POST("/schedule-posters/:id/week", handler.UpdateSchedulePosterWeek)
	holoAPI.POST("/schedule-posters/:id/reject", handler.RejectSchedulePoster)
}

func registerStatsRoutes(holoAPI *gin.RouterGroup, statsHandler *api.StatsHandler) {
	holoAPI.GET("/stats", statsHandler.GetStats)
}

func registerStreamRoutes(holoAPI *gin.RouterGroup, handler *api.StreamHandler) {
	holoAPI.GET("/streams/live", handler.GetLiveStreams)
	holoAPI.GET("/streams/upcoming", handler.GetUpcomingStreams)
}
