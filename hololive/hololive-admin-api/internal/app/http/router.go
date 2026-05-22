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
	appConfig *config.Config,
	logger *slog.Logger,
	domainHandlers *server.DomainHandlers,
	authHandler *server.AuthHandler,
	webhookHandler *iris.WebhookHandler,
	triggerHandler *sharedserver.TriggerHandler,
	cacheClient cache.Client,
) (*gin.Engine, error) {
	if err := validateAPIRouterInputs(appConfig, domainHandlers, authHandler); err != nil {
		return nil, err
	}
	if logger == nil {
		logger = slog.Default()
	}

	router, err := newAPIRouter(ctx, appConfig, logger)
	if err != nil {
		return nil, err
	}

	if webhookHandler != nil {
		router.POST("/webhook/iris", gin.WrapH(webhookHandler))
	}

	if triggerHandler != nil {
		triggerHandler.RegisterInternalRoutesWithAuth(router.Group(""), appConfig.Server.APIKey)
	}

	registerAPIRoutes(router, appConfig.Server.APIKey, cacheClient, logger, domainHandlers, authHandler)

	logger.Info("api_key_auth_enabled")

	return router, nil
}

func validateAPIRouterInputs(
	appConfig *config.Config,
	domainHandlers *server.DomainHandlers,
	authHandler *server.AuthHandler,
) error {
	if appConfig == nil {
		return errors.New("config must not be nil")
	}
	if strings.TrimSpace(appConfig.Server.APIKey) == "" {
		return errors.New("API_SECRET_KEY required")
	}
	if domainHandlers == nil {
		return errors.New("domain handlers must not be nil")
	}
	if err := validateDomainHandlers(domainHandlers); err != nil {
		return err
	}
	if authHandler == nil {
		return errors.New("auth handler must not be nil")
	}
	return nil
}

func validateDomainHandlers(h *server.DomainHandlers) error {
	requiredHandlers := []struct {
		missing bool
		err     string
	}{
		{h.Member == nil, "member handler must not be nil"},
		{h.Alarm == nil, "alarm handler must not be nil"},
		{h.Room == nil, "room handler must not be nil"},
		{h.Stream == nil, "stream handler must not be nil"},
		{h.Stats == nil, "stats handler must not be nil"},
		{h.Settings == nil, "settings handler must not be nil"},
		{h.Template == nil, "template handler must not be nil"},
		{h.Milestone == nil, "milestone handler must not be nil"},
		{h.Profile == nil, "profile handler must not be nil"},
		{h.MajorEvent == nil, "major event handler must not be nil"},
		{h.OAuth == nil, "oauth handler must not be nil"},
	}
	for _, required := range requiredHandlers {
		if required.missing {
			return errors.New(required.err)
		}
	}
	return nil
}

func newAPIRouter(ctx context.Context, appConfig *config.Config, logger *slog.Logger) (*gin.Engine, error) {
	if appConfig == nil {
		return nil, errors.New("config must not be nil")
	}
	if logger == nil {
		logger = slog.Default()
	}

	isProduction := strings.EqualFold(strings.TrimSpace(appConfig.Environment), "production")
	if err := validateAPICORSConfig(appConfig, isProduction); err != nil {
		return nil, err
	}

	router, err := sharedserver.NewRuntimeRouter(ctx, logger, sharedserver.RuntimeRouterOptions{
		APIKey:       appConfig.Server.APIKey,
		EnableGzip:   true,
		SkipLogPaths: []string{"/metrics"},
		PreRouteUse: []gin.HandlerFunc{
			corsOriginGuard(appConfig.CORS.AllowedOrigins, appConfig.CORS.Enforce, logger),
			cors.New(newAPICORSConfig(appConfig, appConfig.CORS.Enforce)),
			middleware.ClientHintsMiddleware(),
		},
	})
	if err != nil {
		return nil, err
	}

	warnMissingProductionCORS(logger, appConfig, isProduction)

	router.NoRoute(middleware.NoRouteAuthHandler(appConfig.Server.APIKey))

	return router, nil
}

func validateAPICORSConfig(appConfig *config.Config, isProduction bool) error {
	origins := normalizedOrigins(appConfig.CORS.AllowedOrigins)
	if isProduction && appConfig.CORS.Enforce && (len(origins) == 0 || containsWildcard(origins)) {
		return errors.New("explicit CORS_ALLOWED_ORIGINS required in production when CORS_ENFORCE=true")
	}
	return nil
}

func warnMissingProductionCORS(logger *slog.Logger, appConfig *config.Config, isProduction bool) {
	if !isProduction || !appConfig.CORS.MissingInProduction {
		return
	}
	logger.Warn(
		"cors_allowed_origins_missing_in_production_monitor_mode",
		slog.Bool("cors_enforce", appConfig.CORS.Enforce),
		slog.String("next_step", "set CORS_ALLOWED_ORIGINS and enable CORS_ENFORCE"),
	)
}
