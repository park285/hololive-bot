package app

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/park285/shared-go/pkg/ginjson"

	"github.com/kapu/admin-dashboard/internal/holo"
	"github.com/kapu/admin-dashboard/internal/httpx"
)

func (r *Runtime) Handler() http.Handler {
	engine := gin.New()
	engine.HandleMethodNotAllowed = true
	engine.Use(gin.Recovery(), r.securityHeaders(), r.etag())

	engine.GET("/health", r.handleHealth)
	engine.GET("/favicon.svg", gin.WrapF(r.static.ServeFavicon))
	engine.GET("/assets/*filepath", gin.WrapF(r.static.ServeAsset))

	api := engine.Group("/admin/api")
	api.POST("/auth/login", r.handleLogin)

	authed := api.Group("", r.auth())
	authed.GET("/auth/session", r.handleSessionStatus)
	authed.GET("/docker/health", r.handleDockerHealth)
	authed.GET("/docker/containers", r.handleDockerContainers)
	authed.GET("/status", r.handleAggregatedStatus)
	authed.GET("/ws/system-stats", r.handleSystemStatsWS)
	authed.GET("/openapi.json", r.handleOpenAPI)

	holoHandler := holo.Handler{Client: r.holo}
	registerHoloReads(authed, holoHandler)

	csrfed := authed.Group("", r.csrf())
	csrfed.POST("/auth/logout", r.handleLogout)
	csrfed.POST("/auth/heartbeat", r.handleHeartbeat)
	csrfed.POST("/docker/containers/:name/restart", r.handleDockerRestart)
	csrfed.POST("/docker/containers/:name/stop", r.handleDockerStop)
	csrfed.POST("/docker/containers/:name/start", r.handleDockerStart)
	registerHoloMutations(csrfed, holoHandler)

	engine.GET("/admin/docs", r.auth(), r.handleDocs)

	engine.NoMethod(func(c *gin.Context) {
		ginjson.Respond(c, http.StatusMethodNotAllowed, httpx.ErrorResponse{Error: "Method not allowed"})
	})
	engine.NoRoute(r.handleFallback)
	return engine
}

func registerHoloReads(group *gin.RouterGroup, h holo.Handler) {
	group.GET("/holo/alarms", h.ProxyGet("/api/holo/alarms", nil))
	group.GET("/holo/members", h.ProxyGet("/api/holo/members", nil))
	group.GET("/holo/rooms", h.ProxyGet("/api/holo/rooms", nil))
	group.GET("/holo/settings", h.ProxyGet("/api/holo/settings", nil))
	group.GET("/holo/stats", h.ProxyGet("/api/holo/stats", nil))
	group.GET("/holo/stats/channels", h.ChannelStats)
	group.GET("/holo/stats/youtube/community-shorts", h.ProxyGet("/api/holo/stats/youtube/community-shorts", nil))
	group.GET("/holo/streams/live", h.ProxyGet("/api/holo/streams/live", holo.PassOnly("org")))
	group.GET("/holo/streams/upcoming", h.ProxyGet("/api/holo/streams/upcoming", holo.PassOnly("org")))
	group.GET("/holo/milestones", h.ProxyGet("/api/holo/milestones", holo.PassOnly("limit", "offset", "channelId", "memberName")))
	group.GET("/holo/milestones/near", h.ProxyGet("/api/holo/milestones/near", holo.PassOnly("threshold")))
	group.GET("/holo/milestones/stats", h.ProxyGet("/api/holo/milestones/stats", nil))
	group.GET("/holo/members/calendar", h.ProxyGet("/api/holo/members/calendar", holo.PassOnly("year", "month")))
}

func registerHoloMutations(group *gin.RouterGroup, h holo.Handler) {
	group.DELETE("/holo/alarms", h.ProxyMutation(http.MethodDelete, "/api/holo/alarms"))
	group.POST("/holo/members", h.ProxyMutation(http.MethodPost, "/api/holo/members"))
	group.POST("/holo/members/:id/aliases", h.ProxyMemberMutation(http.MethodPost, "/aliases"))
	group.DELETE("/holo/members/:id/aliases", h.ProxyMemberMutation(http.MethodDelete, "/aliases"))
	group.PATCH("/holo/members/:id/graduation", h.ProxyMemberMutation(http.MethodPatch, "/graduation"))
	group.PATCH("/holo/members/:id/channel", h.ProxyMemberMutation(http.MethodPatch, "/channel"))
	group.PATCH("/holo/members/:id/name", h.ProxyMemberMutation(http.MethodPatch, "/name"))
	group.POST("/holo/rooms", h.ProxyMutation(http.MethodPost, "/api/holo/rooms"))
	group.DELETE("/holo/rooms", h.ProxyMutation(http.MethodDelete, "/api/holo/rooms"))
	group.POST("/holo/rooms/acl", h.ProxyMutation(http.MethodPost, "/api/holo/rooms/acl"))
	group.POST("/holo/settings", h.ProxyMutation(http.MethodPost, "/api/holo/settings"))
	group.POST("/holo/names/room", h.ProxyMutation(http.MethodPost, "/api/holo/names/room"))
	group.POST("/holo/names/user", h.ProxyMutation(http.MethodPost, "/api/holo/names/user"))
}

func (r *Runtime) handleFallback(c *gin.Context) {
	if strings.HasPrefix(c.Request.URL.Path, "/admin/api/") {
		ginjson.Respond(c, http.StatusNotFound, httpx.ErrorResponse{Error: "Not found"})
		return
	}
	r.static.ServeIndex(c.Writer, c.Request)
}
