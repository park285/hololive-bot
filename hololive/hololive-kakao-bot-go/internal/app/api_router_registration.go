package app

import (
	"github.com/gin-gonic/gin"
	"github.com/kapu/hololive-shared/pkg/server/middleware"

	"github.com/kapu/hololive-kakao-bot-go/internal/server"
)

func registerAPIRoutes(
	router *gin.Engine,
	apiKey string,
	domainHandlers *server.DomainAPIHandlers,
	authHandler *server.AuthHandler,
) {
	domains := domainHandlers

	// OAuth 콜백 프록시 (인증 불필요 - Google에서 직접 호출)
	// 모바일 앱에서 localhost 리디렉션이 불가능하므로 서버가 프록시 역할
	router.GET("/oauth/callback", domains.OAuth.OAuthCallbackHandler)

	// 공개 스트림 조회 API
	publicHoloAPI := router.Group("/api/holo")
	registerPublicStreamRoutes(publicHoloAPI, domains.Stream)

	// Session 기반 인증 API
	authAPI := router.Group("/api/auth")
	authAPI.POST("/register", authHandler.Register)
	authAPI.POST("/login", authHandler.Login)
	authAPI.POST("/logout", authHandler.Logout)
	authAPI.POST("/refresh", authHandler.Refresh)
	authAPI.GET("/me", authHandler.Me)
	authAPI.POST("/password/reset-request", authHandler.ResetRequest)
	authAPI.POST("/password/reset", authHandler.ResetPassword)

	// hololive-bot 도메인 API (Admin Dashboard, Tauri 앱에서 사용)
	holoAPI := router.Group("/api/holo")

	// API Key 인증 미들웨어 적용 (apiKey가 빈 문자열이면 인증 건너뜀)
	holoAPI.Use(middleware.APIKeyAuthMiddleware(apiKey))

	registerMemberRoutes(holoAPI, domains.Member)
	registerAlarmRoutes(holoAPI, domains.Alarm)
	registerRoomRoutes(holoAPI, domains.Room)
	registerStatsRoutes(holoAPI, domains.Stats, domains.Stream)
	registerSettingsRoutes(holoAPI, domains.Settings)
	registerTemplateRoutes(holoAPI, domains.Template)
	registerMilestoneRoutes(holoAPI, domains.Milestone)
	registerProfileRoutes(holoAPI, domains.Profile)
	registerMajorEventRoutes(holoAPI, domains.MajorEvent)
}
