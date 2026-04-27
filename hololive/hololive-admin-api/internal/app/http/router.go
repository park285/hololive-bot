// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package apphttp

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/kapu/hololive-shared/pkg/config"
	sharedserver "github.com/kapu/hololive-shared/pkg/server"
	"github.com/kapu/hololive-shared/pkg/server/middleware"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/park285/iris-client-go/iris"

	"github.com/kapu/hololive-admin-api/internal/server"
)

// Admin Dashboard와 Tauri 앱에서 사용됩니다.
func ProvideAPIRouter(
	ctx context.Context,
	cfg *config.Config,
	logger *slog.Logger,
	domainHandlers *server.DomainAPIHandlers,
	authHandler *server.AuthHandler,
	webhookHandler *iris.WebhookHandler,
	triggerHandler *sharedserver.TriggerHandler,
	cacheSvc cache.Client,
) (*gin.Engine, error) {
	if cfg == nil {
		return nil, errors.New("config must not be nil")
	}
	if logger == nil {
		logger = slog.Default()
	}
	if strings.TrimSpace(cfg.Server.APIKey) == "" {
		return nil, errors.New("API_SECRET_KEY required")
	}
	if domainHandlers == nil {
		return nil, errors.New("domain handlers must not be nil")
	}
	if err := validateDomainHandlers(domainHandlers); err != nil {
		return nil, err
	}
	if authHandler == nil {
		return nil, errors.New("auth handler must not be nil")
	}

	router, err := newAPIRouter(ctx, cfg, logger)
	if err != nil {
		return nil, err
	}

	if webhookHandler != nil {
		router.POST("/webhook/iris", gin.WrapH(webhookHandler))
	}

	if triggerHandler != nil {
		triggerHandler.RegisterInternalRoutesWithAuth(router.Group(""), cfg.Server.APIKey)
	}

	registerAPIRoutes(router, cfg.Server.APIKey, cacheSvc, logger, domainHandlers, authHandler)

	logger.Info("api_key_auth_enabled")

	return router, nil
}

func validateDomainHandlers(h *server.DomainAPIHandlers) error {
	switch {
	case h.Member == nil:
		return errors.New("member handler must not be nil")
	case h.Alarm == nil:
		return errors.New("alarm handler must not be nil")
	case h.Room == nil:
		return errors.New("room handler must not be nil")
	case h.Stream == nil:
		return errors.New("stream handler must not be nil")
	case h.Stats == nil:
		return errors.New("stats handler must not be nil")
	case h.Settings == nil:
		return errors.New("settings handler must not be nil")
	case h.Template == nil:
		return errors.New("template handler must not be nil")
	case h.Milestone == nil:
		return errors.New("milestone handler must not be nil")
	case h.Profile == nil:
		return errors.New("profile handler must not be nil")
	case h.MajorEvent == nil:
		return errors.New("major event handler must not be nil")
	case h.OAuth == nil:
		return errors.New("oauth handler must not be nil")
	default:
		return nil
	}
}

func newAPIRouter(ctx context.Context, cfg *config.Config, logger *slog.Logger) (*gin.Engine, error) {
	if cfg == nil {
		return nil, errors.New("config must not be nil")
	}
	if logger == nil {
		logger = slog.Default()
	}

	isProduction := strings.EqualFold(strings.TrimSpace(cfg.Environment), "production")
	origins := normalizedOrigins(cfg.CORS.AllowedOrigins)
	if isProduction && cfg.CORS.Enforce && (len(origins) == 0 || containsWildcard(origins)) {
		return nil, errors.New("explicit CORS_ALLOWED_ORIGINS required in production when CORS_ENFORCE=true")
	}

	router, err := sharedserver.NewRuntimeRouter(ctx, logger, sharedserver.RuntimeRouterOptions{
		APIKey:       cfg.Server.APIKey,
		EnableGzip:   true,
		SkipLogPaths: []string{"/metrics"},
		PreRouteUse: []gin.HandlerFunc{
			corsOriginGuard(cfg.CORS.AllowedOrigins, cfg.CORS.Enforce, logger),
			cors.New(newAPICORSConfig(cfg, cfg.CORS.Enforce)),
			middleware.ClientHintsMiddleware(),
		},
	})
	if err != nil {
		return nil, err
	}

	if isProduction && cfg.CORS.MissingInProduction {
		logger.Warn(
			"cors_allowed_origins_missing_in_production_monitor_mode",
			slog.Bool("cors_enforce", cfg.CORS.Enforce),
			slog.String("next_step", "set CORS_ALLOWED_ORIGINS and enable CORS_ENFORCE"),
		)
	}

	router.NoRoute(middleware.NoRouteAuthHandler(cfg.Server.APIKey))

	return router, nil
}
