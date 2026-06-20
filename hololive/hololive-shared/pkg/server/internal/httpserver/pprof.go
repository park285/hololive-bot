package httpserver

import (
	"context"
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
	group.Use(middleware.APIKeyAuthMiddleware(apiKey))
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
