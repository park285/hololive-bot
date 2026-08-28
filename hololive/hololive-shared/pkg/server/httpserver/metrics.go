package httpserver

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/park285/shared-go/v2/pkg/workercontract"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/workerobservability"
)

// 운영 표면이 H3 전용이라 Prometheus가 직접 scrape하지 못해,
// /metrics만 노출하는 평문 HTTP/1.1 리스너를 분리한다(PR-P6-01 0단계).
func NewMetricsServer(ctx context.Context, addr, metricsKey string, registries ...*workercontract.Registry) *http.Server {
	router := newReleaseModeEngine()
	ApplyBaseMiddleware(ctx, router, nil, BaseMiddlewareOptions{
		SkipLogPaths: []string{"/metrics"},
	})

	metrics := router.Group("")
	metrics.Use(loopbackAwareAuthMiddleware(addr, metricsKey))

	if len(registries) == 1 && registries[0] != nil {
		metrics.GET("/metrics", gin.WrapH(promhttp.HandlerFor(workerobservability.NewGatherer(registries[0]), promhttp.HandlerOpts{})))
		metrics.GET("/diagnostics/workers", gin.WrapH(workerobservability.DiagnosticsHandler(registries[0])))
	} else {
		metrics.GET("/metrics", gin.WrapH(promhttp.Handler()))
	}

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
