package httpserver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/gin-contrib/gzip"
	"github.com/gin-gonic/gin"
	"github.com/park285/shared-go/v2/pkg/health"
	"github.com/park285/shared-go/v2/pkg/httputil"
	"github.com/park285/shared-go/v2/pkg/telemetry"
	"github.com/park285/shared-go/v2/pkg/workercontract"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/kapu/hololive-shared/internal/workerobservability"
	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/server/middleware"
)

type RuntimeRouterOptions struct {
	APIKey                 string
	DisableMetricsAuth     bool
	EnableGzip             bool
	Operation              string
	SkipLogPaths           []string
	PreRouteUse            []gin.HandlerFunc
	RegisterRoutes         func(*gin.Engine) error
	ReadyResponder         func(*gin.Context)
	InternalReadyResponder func(*gin.Context)
	WorkerRegistry         *workercontract.Registry

	// TrustRemoteAddrOnly가 true이면 c.ClientIP()가 TCP RemoteAddr만 반영하도록
	// TrustedPlatform과 trusted proxy를 모두 비운다. CF-Connecting-IP/X-Forwarded-For 등
	// 위조 가능한 헤더를 무시해야 하는 직결(예: Tailscale) 형상에서만 켠다.
	// zero value(false)는 기존 동작(gin.PlatformCloudflare)을 유지한다.
	TrustRemoteAddrOnly bool
}

func NewHealthOnlyRuntimeRouter(
	ctx context.Context,
	logger *slog.Logger,
	apiKey string,
	opts ...func(*RuntimeRouterOptions),
) (*gin.Engine, error) {
	options := RuntimeRouterOptions{APIKey: apiKey}

	for _, opt := range opts {
		if opt != nil {
			opt(&options)
		}
	}

	out, err := NewRuntimeRouter(ctx, logger, &options)
	if err != nil {
		return nil, fmt.Errorf("runtime router: %w", err)
	}

	return out, nil
}

func NewTriggerRuntimeRouter(
	ctx context.Context,
	logger *slog.Logger,
	triggerHandler *TriggerHandler,
	apiKey string,
	opts ...func(*RuntimeRouterOptions),
) (*gin.Engine, error) {
	options := RuntimeRouterOptions{
		APIKey:         apiKey,
		RegisterRoutes: triggerRuntimeRouteRegistrar(triggerHandler, apiKey),
	}
	applyRuntimeRouterOptions(&options, opts)

	out, err := NewRuntimeRouter(ctx, logger, &options)
	if err != nil {
		return nil, fmt.Errorf("runtime router: %w", err)
	}

	return out, nil
}

func triggerRuntimeRouteRegistrar(triggerHandler *TriggerHandler, apiKey string) func(*gin.Engine) error {
	return func(router *gin.Engine) error {
		if triggerHandler == nil {
			return nil
		}

		if strings.TrimSpace(apiKey) == "" {
			return errors.New("API_SECRET_KEY required")
		}

		triggerHandler.RegisterInternalRoutesWithAuth(router.Group(""), apiKey)

		return nil
	}
}

func NewHTTPServer(addr string, handler http.Handler, operation string,
	traceFilters ...func(*http.Request) bool,
) *http.Server {
	if handler == nil {
		handler = http.NotFoundHandler()
	}

	traceFilter := firstTraceFilter(traceFilters)

	handler = telemetry.NewPublicHTTPHandler(handler, operation, telemetry.HTTPHandlerOptions{Filter: traceFilter})

	srv := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: constants.ServerTimeout.ReadHeader,
		ReadTimeout:       constants.ServerTimeout.Read,
		WriteTimeout:      constants.ServerTimeout.Write,
		IdleTimeout:       constants.ServerTimeout.Idle,
		MaxHeaderBytes:    constants.ServerTimeout.MaxHeaderBytes,
	}

	return srv
}

func NewRuntimeRouter(ctx context.Context, logger *slog.Logger, opts *RuntimeRouterOptions) (*gin.Engine, error) {
	if opts == nil {
		opts = &RuntimeRouterOptions{}
	}

	router := newReleaseModeEngine()
	if err := configureRuntimeClientIPTrust(router, opts.TrustRemoteAddrOnly); err != nil {
		return nil, fmt.Errorf("configure runtime client IP trust: %w", err)
	}

	installRuntimeMiddleware(ctx, router, logger, opts)

	router.GET("/health", func(c *gin.Context) {
		respondJSON(c, http.StatusOK, health.Get())
	})
	registerRuntimeReadyRoute(router, opts.ReadyResponder)
	registerRuntimeInternalReadyRoute(router, opts.APIKey, opts.InternalReadyResponder)

	metrics := router.Group("")
	metrics.Use(middleware.AuthMiddleware(httputil.AdminAuthConfig{
		APIKey:   opts.APIKey,
		Disabled: opts.DisableMetricsAuth,
	}))

	if opts.WorkerRegistry != nil {
		metrics.GET("/metrics", gin.WrapH(promhttp.HandlerFor(workerobservability.NewGatherer(opts.WorkerRegistry), promhttp.HandlerOpts{})))
		metrics.GET("/diagnostics/workers", gin.WrapH(workerobservability.DiagnosticsHandler(opts.WorkerRegistry)))
	} else {
		metrics.GET("/metrics", gin.WrapH(promhttp.Handler()))
	}

	if opts.RegisterRoutes != nil {
		if err := opts.RegisterRoutes(router); err != nil {
			return nil, fmt.Errorf("register routes: %w", err)
		}
	}

	return router, nil
}

// configureRuntimeClientIPTrust는 c.ClientIP()의 신뢰 소스를 설정한다.
// 옵션 trustRemoteAddrOnly가 true이면 TrustedPlatform과 trusted proxy를 비워
// 위조 가능한 헤더를 무시하고 TCP RemoteAddr만 신뢰한다(직결 형상).
// 그렇지 않으면 기존 Cloudflare 형상(trusted proxies + PlatformCloudflare)을 유지한다.
func configureRuntimeClientIPTrust(router *gin.Engine, trustRemoteAddrOnly bool) error {
	if trustRemoteAddrOnly {
		if err := router.SetTrustedProxies(nil); err != nil {
			return fmt.Errorf("set trusted proxies: %w", err)
		}

		router.TrustedPlatform = ""

		return nil
	}

	if err := router.SetTrustedProxies(constants.ServerConfig.TrustedProxies); err != nil {
		return fmt.Errorf("set trusted proxies: %w", err)
	}

	router.TrustedPlatform = gin.PlatformCloudflare

	return nil
}

func applyRuntimeRouterOptions(options *RuntimeRouterOptions, opts []func(*RuntimeRouterOptions)) {
	for _, opt := range opts {
		if opt != nil {
			opt(options)
		}
	}
}

func installRuntimeMiddleware(ctx context.Context, router *gin.Engine, logger *slog.Logger, opts *RuntimeRouterOptions) {
	if opts == nil {
		opts = &RuntimeRouterOptions{}
	}

	// "/__observability/*"는 로깅에서만 제외한다. observability-stack의
	// collect-runtime-metrics.sh가 liveness 근거로 쓸 404 span을 만들려고 일부러
	// 호출하는 경로이므로, LocalPlaneTraceFilter에는 절대 추가하면 안 된다.
	ApplyBaseMiddleware(ctx, router, logger, BaseMiddlewareOptions{
		SkipLogPaths: append(
			[]string{"/health", "/ready", "/internal/ready", "/metrics", "/__observability/*"},
			opts.SkipLogPaths...),
	})

	if opts.EnableGzip {
		router.Use(gzip.Gzip(gzip.DefaultCompression))
	}

	for _, use := range opts.PreRouteUse {
		if use != nil {
			router.Use(use)
		}
	}
}

func registerRuntimeReadyRoute(router *gin.Engine, readyResponder func(*gin.Context)) {
	if readyResponder != nil {
		router.GET("/ready", readyResponder)

		return
	}

	router.GET("/ready", func(c *gin.Context) {
		response, ready := health.GetReadiness()
		status := http.StatusOK

		if !ready {
			status = http.StatusServiceUnavailable
		}

		respondJSON(c, status, response)
	})
}

func registerRuntimeInternalReadyRoute(router *gin.Engine, apiKey string, readyResponder func(*gin.Context)) {
	if readyResponder == nil || strings.TrimSpace(apiKey) == "" {
		return
	}

	internal := router.Group("/internal")
	internal.Use(middleware.APIKeyAuthMiddleware(apiKey))
	internal.GET("/ready", readyResponder)
}

func LocalPlaneTraceFilter(r *http.Request) bool {
	switch r.URL.Path {
	case "/health", "/ready", "/internal/ready", "/metrics", "/diagnostics/workers":
		return false
	default:
		return true
	}
}

func firstTraceFilter(filters []func(*http.Request) bool) func(*http.Request) bool {
	for _, filter := range filters {
		if filter != nil {
			return filter
		}
	}

	return nil
}
