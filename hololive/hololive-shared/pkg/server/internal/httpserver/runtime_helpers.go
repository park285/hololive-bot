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
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/health"
	"github.com/kapu/hololive-shared/pkg/server/middleware"
)

type RuntimeRouterOptions struct {
	APIKey         string
	EnableGzip     bool
	Operation      string
	SkipLogPaths   []string
	PreRouteUse    []gin.HandlerFunc
	RegisterRoutes func(*gin.Engine) error
	ReadyResponder func(*gin.Context)

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
	return NewRuntimeRouter(ctx, logger, options)
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
	return NewRuntimeRouter(ctx, logger, options)
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

func NewH2CServer(addr string, handler http.Handler, operation string) *http.Server {
	if handler == nil {
		handler = http.NotFoundHandler()
	}
	if strings.TrimSpace(operation) != "" {
		handler = otelhttp.NewHandler(handler, operation)
	}

	srv := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: constants.ServerTimeout.ReadHeader,
		ReadTimeout:       constants.ServerTimeout.Read,
		WriteTimeout:      constants.ServerTimeout.Write,
		IdleTimeout:       constants.ServerTimeout.Idle,
		MaxHeaderBytes:    constants.ServerTimeout.MaxHeaderBytes,
	}
	EnableH2C(srv)
	return srv
}

func NewRuntimeRouter(ctx context.Context, logger *slog.Logger, opts RuntimeRouterOptions) (*gin.Engine, error) {
	gin.SetMode(gin.ReleaseMode)

	router := gin.New()
	if err := configureRuntimeClientIPTrust(router, opts.TrustRemoteAddrOnly); err != nil {
		return nil, err
	}

	installRuntimeMiddleware(router, ctx, logger, opts)

	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, health.Get())
	})
	registerRuntimeReadyRoute(router, opts.ReadyResponder)

	metrics := router.Group("")
	metrics.Use(middleware.APIKeyAuthMiddleware(opts.APIKey))
	metrics.GET("/metrics", gin.WrapH(promhttp.Handler()))

	if opts.RegisterRoutes != nil {
		if err := opts.RegisterRoutes(router); err != nil {
			return nil, err
		}
	}

	return router, nil
}

// configureRuntimeClientIPTrust는 c.ClientIP()의 신뢰 소스를 설정한다.
// trustRemoteAddrOnly=true이면 TrustedPlatform과 trusted proxy를 비워
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

func installRuntimeMiddleware(router *gin.Engine, ctx context.Context, logger *slog.Logger, opts RuntimeRouterOptions) {
	router.Use(gin.Recovery())
	if opts.EnableGzip {
		router.Use(gzip.Gzip(gzip.DefaultCompression))
	}
	ApplyBaseMiddleware(router, ctx, logger, BaseMiddlewareOptions{
		SkipLogPaths: append([]string{"/health", "/ready", "/metrics"}, opts.SkipLogPaths...),
	})
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
		c.JSON(http.StatusOK, health.Get())
	})
}
