// Package server: HTTP 서버 및 라우팅
package server

import (
	"github.com/gin-gonic/gin"

	"github.com/park285/llm-kakao-bots/admin-dashboard/internal/auth"
	"github.com/park285/llm-kakao-bots/admin-dashboard/internal/middleware"
)

func (s *Server) setupRoutes() {
	api := s.engine.Group("/admin/api")

	// 도메인별 라우터 설정
	s.setupAuthRoutes(api)

	// 인증 필요 라우트
	authenticated := api.Group("")
	authenticated.Use(auth.AuthMiddleware(s.sessions, s.cfg.AdminSecretKey, s.cfg.ForceHTTPS))

	s.setupDockerRoutes(authenticated)
	s.setupLogsRoutes(authenticated)
	s.setupStatusRoutes(authenticated)
	s.setupProxyRoutes(authenticated)

	// Health & Static
	s.setupHealthRoute()
	s.setupStaticRoutes()
}

// setupAuthRoutes: 인증 관련 라우트 (미들웨어 없음)
func (s *Server) setupAuthRoutes(api *gin.RouterGroup) {
	authGroup := api.Group("/auth")
	authGroup.POST("/login", s.handleLogin)
	csrfMiddleware := middleware.CSRFProtectionWithMode(s.cfg.AdminSecretKey, string(s.securityCfg.CSRFMode), s.logger)
	authGroup.POST("/logout", csrfMiddleware, s.handleLogout)
	authGroup.POST("/heartbeat", csrfMiddleware, s.handleHeartbeat)
}

// setupDockerRoutes: Docker 컨테이너 관리 라우트
func (s *Server) setupDockerRoutes(authenticated *gin.RouterGroup) {
	dockerGroup := authenticated.Group("/docker")
	dockerGroup.GET("/health", s.handleDockerHealth)
	dockerGroup.GET("/containers", s.handleDockerContainers)

	// CSRF 보호는 상태 변경(POST) 라우트에만 적용한다.
	// - 그룹 전체에 걸면, 향후 POST 기반 인증/유지 엔드포인트에 영향을 줄 수 있음
	// - 3상태 모드 지원: enforce/monitor/off
	csrfMiddleware := middleware.CSRFProtectionWithMode(s.cfg.AdminSecretKey, string(s.securityCfg.CSRFMode), s.logger)
	dockerGroup.POST("/containers/:name/restart", csrfMiddleware, s.handleDockerRestart)
	dockerGroup.POST("/containers/:name/stop", csrfMiddleware, s.handleDockerStop)
	dockerGroup.POST("/containers/:name/start", csrfMiddleware, s.handleDockerStart)
	dockerGroup.GET("/containers/:name/logs/stream", s.handleDockerLogStream)
}

// setupLogsRoutes: 시스템 로그 라우트
func (s *Server) setupLogsRoutes(authenticated *gin.RouterGroup) {
	logsGroup := authenticated.Group("/logs")
	logsGroup.GET("/files", s.handleLogFiles)
	logsGroup.GET("", s.handleSystemLogs)
}

// setupStatusRoutes: 통합 시스템 상태 라우트
func (s *Server) setupStatusRoutes(authenticated *gin.RouterGroup) {
	statusGroup := authenticated.Group("/status")
	statusGroup.GET("", s.handleAggregatedStatus)

	// WebSocket: 실시간 시스템 리소스 스트리밍 (CPU, Memory, Goroutines)
	// 기존 /admin/api/holo/ws/system-stats → /admin/api/ws/system-stats로 이관
	wsGroup := authenticated.Group("/ws")
	wsGroup.GET("/system-stats", s.handleSystemStatsStream)
}

// setupProxyRoutes: 도메인 봇 프록시 라우트
func (s *Server) setupProxyRoutes(authenticated *gin.RouterGroup) {
	if s.botProxies == nil {
		return
	}

	// 도메인별 프록시
	authenticated.Any("/holo/*path", s.botProxies.ProxyHolo)
}
