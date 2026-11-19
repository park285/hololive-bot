package httpserver

import (
	"context"
	"net"
	"net/http"
	"net/http/pprof"

	"github.com/gin-gonic/gin"

	"github.com/kapu/hololive-shared/pkg/server/middleware"
)

func NewPprofServer(ctx context.Context, addr, apiKey string) *http.Server {
	router := newReleaseModeEngine()
	applyBaseMiddleware(ctx, router, nil, "/debug/pprof*")

	group := router.Group("/debug/pprof")
	group.Use(loopbackAwareAuthMiddleware(addr, apiKey))
	group.GET("/", gin.WrapF(pprof.Index))
	group.GET("/cmdline", gin.WrapF(pprof.Cmdline))
	group.GET("/profile", gin.WrapF(pprof.Profile))
	group.GET("/symbol", gin.WrapF(pprof.Symbol))
	group.POST("/symbol", gin.WrapF(pprof.Symbol))
	group.GET("/trace", gin.WrapF(pprof.Trace))
	group.GET("/:profile", gin.WrapF(pprof.Index))

	return &http.Server{
		Addr:              addr,
		Handler:           router,
		ReadHeaderTimeout: serverReadHeaderTimeout,
		MaxHeaderBytes:    serverMaxHeaderBytes,
	}
}

func loopbackAwareAuthMiddleware(addr, apiKey string) gin.HandlerFunc {
	if apiKey != "" {
		return middleware.APIKeyAuthMiddleware(apiKey)
	}

	if isLoopbackListenAddr(addr) {
		return func(c *gin.Context) { c.Next() }
	}

	return func(c *gin.Context) { c.AbortWithStatus(http.StatusForbidden) }
}

func isLoopbackListenAddr(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}

	if host == "localhost" {
		return true
	}

	ip := net.ParseIP(host)

	return ip != nil && ip.IsLoopback()
}
