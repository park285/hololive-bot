package server

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
		APIKey: apiKey,
		RegisterRoutes: func(router *gin.Engine) error {
			if triggerHandler == nil {
				return nil
			}
			if strings.TrimSpace(apiKey) == "" {
				return errors.New("API_SECRET_KEY required")
			}
			triggerHandler.RegisterInternalRoutesWithAuth(router.Group(""), apiKey)
			return nil
		},
	}
	for _, opt := range opts {
		if opt != nil {
			opt(&options)
		}
	}
	return NewRuntimeRouter(ctx, logger, options)
}

func NewH2CServer(addr string, handler http.Handler, operation string) *http.Server {
	if handler == nil {
		handler = http.NotFoundHandler()
	}
	if strings.TrimSpace(operation) != "" {
		handler = otelhttp.NewHandler(handler, operation)
	}

	return &http.Server{
		Addr:              addr,
		Handler:           WrapH2C(handler),
		ReadHeaderTimeout: constants.ServerTimeout.ReadHeader,
		ReadTimeout:       constants.ServerTimeout.Read,
		WriteTimeout:      constants.ServerTimeout.Write,
		IdleTimeout:       constants.ServerTimeout.Idle,
		MaxHeaderBytes:    constants.ServerTimeout.MaxHeaderBytes,
	}
}

func NewRuntimeRouter(ctx context.Context, logger *slog.Logger, opts RuntimeRouterOptions) (*gin.Engine, error) {
	gin.SetMode(gin.ReleaseMode)

	router := gin.New()
	if err := router.SetTrustedProxies(constants.ServerConfig.TrustedProxies); err != nil {
		return nil, fmt.Errorf("set trusted proxies: %w", err)
	}
	router.TrustedPlatform = gin.PlatformCloudflare

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

	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, health.Get())
	})
	if opts.ReadyResponder != nil {
		router.GET("/ready", opts.ReadyResponder)
	} else {
		router.GET("/ready", func(c *gin.Context) {
			c.JSON(http.StatusOK, health.Get())
		})
	}

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
