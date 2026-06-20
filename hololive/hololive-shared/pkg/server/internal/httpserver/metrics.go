package httpserver

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/kapu/hololive-shared/pkg/constants"
)

// 운영 표면이 H3 전용이라 Prometheus가 직접 scrape하지 못해,
// /metrics만 노출하는 평문 HTTP/1.1 리스너를 분리한다(PR-P6-01 0단계).
func NewMetricsServer(addr, apiKey string) *http.Server {
	gin.SetMode(gin.ReleaseMode)

	router := gin.New()
	ApplyBaseMiddleware(router, context.Background(), nil, BaseMiddlewareOptions{
		SkipLogPaths: []string{"/metrics"},
	})
	metrics := router.Group("")
	metrics.Use(loopbackAwareAuthMiddleware(addr, apiKey))
	metrics.GET("/metrics", gin.WrapH(promhttp.Handler()))

	return &http.Server{
		Addr:              addr,
		Handler:           router,
		ReadHeaderTimeout: constants.ServerTimeout.ReadHeader,
		ReadTimeout:       constants.ServerTimeout.Read,
		WriteTimeout:      constants.ServerTimeout.Write,
		IdleTimeout:       constants.ServerTimeout.Idle,
		MaxHeaderBytes:    constants.ServerTimeout.MaxHeaderBytes,
	}
}
