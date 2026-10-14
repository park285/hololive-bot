package httpserver

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/park285/shared-go/v2/pkg/logging"
	"github.com/park285/shared-go/v2/pkg/workercontract"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/kapu/hololive-shared/internal/workerobservability"
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

	var registry *workercontract.Registry

	if len(registries) == 1 {
		registry = registries[0]
	}

	metrics.GET("/metrics", gin.WrapH(runtimeMetricsHandler(registry)))

	if registry != nil {
		metrics.GET("/diagnostics/workers", gin.WrapH(workerobservability.DiagnosticsHandler(registry)))
	}

	return &http.Server{
		Addr:              addr,
		Handler:           router,
		ReadHeaderTimeout: serverReadHeaderTimeout,
		ReadTimeout:       serverReadTimeout,
		WriteTimeout:      serverWriteTimeout,
		IdleTimeout:       serverIdleTimeout,
		MaxHeaderBytes:    serverMaxHeaderBytes,
	}
}

func runtimeMetricsHandler(registry *workercontract.Registry) http.Handler {
	gatherer := prometheus.DefaultGatherer

	if registry != nil {
		gatherer = workerobservability.NewGatherer(registry)
	}

	logMetrics := prometheus.NewRegistry()
	logMetrics.MustRegister(
		prometheus.NewCounterFunc(prometheus.CounterOpts{
			Name: "iris_stack_logging_stdout_dropped_total",
			Help: "Log records lost from the stdout copy.",
		}, func() float64 { return float64(logging.SnapshotStdout().Dropped) }),
		prometheus.NewCounterFunc(prometheus.CounterOpts{
			Name: "iris_stack_logging_stdout_truncated_total",
			Help: "Log records shortened in the stdout copy.",
		}, func() float64 { return float64(logging.SnapshotStdout().Truncated) }),
		prometheus.NewCounterFunc(prometheus.CounterOpts{
			Name: "iris_stack_logging_file_write_failed_total",
			Help: "File log writes that returned an error or short write.",
		}, func() float64 { return float64(logging.SnapshotFile().WriteFailed) }),
	)

	return promhttp.HandlerFor(prometheus.Gatherers{gatherer, logMetrics}, promhttp.HandlerOpts{})
}
