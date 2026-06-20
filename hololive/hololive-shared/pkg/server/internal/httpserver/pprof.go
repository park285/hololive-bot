package httpserver

import (
	"context"
	"net"
	"net/http"
	"net/http/pprof"

	"github.com/gin-gonic/gin"

	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/server/middleware"
)

func NewPprofServer(addr, apiKey string) *http.Server {
	gin.SetMode(gin.ReleaseMode)

	router := gin.New()
	ApplyBaseMiddleware(router, context.Background(), nil, BaseMiddlewareOptions{
		SkipLogPaths: []string{"/debug/pprof"},
	})
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
		Addr:    addr,
		Handler: router,
		// /debug/pprof/profile?seconds=N은 N초간 블로킹 응답을 스트리밍하므로
		// Read/WriteTimeout을 두면 프로파일이 잘린다. ReadHeaderTimeout만 건다.
		ReadHeaderTimeout: constants.ServerTimeout.ReadHeader,
		MaxHeaderBytes:    constants.ServerTimeout.MaxHeaderBytes,
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
