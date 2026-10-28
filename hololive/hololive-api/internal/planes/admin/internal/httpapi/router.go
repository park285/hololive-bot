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

package httpapi

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/gin-gonic/gin"

	apiconfig "github.com/kapu/hololive-api/internal/config"
	server "github.com/kapu/hololive-api/internal/planes/admin/internal/server/api"
	"github.com/kapu/hololive-api/internal/readiness"
	sharedreadiness "github.com/kapu/hololive-shared/pkg/readiness"
	sharedserver "github.com/kapu/hololive-shared/pkg/server/httpserver"
	"github.com/kapu/hololive-shared/pkg/server/middleware"
	"github.com/kapu/hololive-shared/pkg/service/cache"
)

func ProvideAPIRouter(
	ctx context.Context,
	appConfig *apiconfig.AdminPlaneConfig,
	logger *slog.Logger,
	domainHandlers *server.DomainHandlers,
	cacheClient cache.LowLevelCache,
	readyProbe ...*sharedreadiness.Probe,
) (*gin.Engine, error) {
	if err := validateAPIRouterInputs(appConfig, domainHandlers); err != nil {
		return nil, fmt.Errorf("validate API router inputs: %w", err)
	}

	if logger == nil {
		logger = slog.Default()
	}

	router, err := newAPIRouter(ctx, appConfig, logger, readyProbe...)
	if err != nil {
		return nil, fmt.Errorf("API router: %w", err)
	}

	if err := registerAPIRoutes(router, appConfig.Server.APIKey, cacheClient, logger, domainHandlers); err != nil {
		return nil, fmt.Errorf("register API routes: %w", err)
	}

	logger.Info("api_key_auth_enabled")

	return router, nil
}

func validateAPIRouterInputs(
	appConfig *apiconfig.AdminPlaneConfig,
	domainHandlers *server.DomainHandlers,
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
		return fmt.Errorf("validate domain handlers: %w", err)
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
	}
	for _, required := range requiredHandlers {
		if required.missing {
			return errors.New(required.err)
		}
	}

	return nil
}

func newAPIRouter(ctx context.Context, appConfig *apiconfig.AdminPlaneConfig, logger *slog.Logger, readyProbe ...*sharedreadiness.Probe) (*gin.Engine, error) {
	if appConfig == nil {
		return nil, errors.New("config must not be nil")
	}

	if logger == nil {
		logger = slog.Default()
	}

	routerOptions := &sharedserver.RuntimeRouterOptions{
		APIKey:       appConfig.Server.APIKey,
		EnableGzip:   true,
		SkipLogPaths: []string{"/metrics"},
		// admin-api는 Tailscale 직결(reverse-proxy 없음)이라 신뢰 가능한 클라이언트 IP는
		// TCP RemoteAddr 뿐이다. CF-Connecting-IP/X-Forwarded-For 위조로 IP 기준 rate limit을
		// 우회하지 못하도록 RemoteAddr만 신뢰한다.
		TrustRemoteAddrOnly: true,
		// admin API는 Iris Console gateway가 서버 대 서버로 API key를 붙여 호출하므로 브라우저 CORS 표면이 없다.
		PreRouteUse: []gin.HandlerFunc{
			middleware.ClientHintsMiddleware(),
		},
	}

	if probe := readiness.Pick(readyProbe...); probe != nil {
		routerOptions.InternalReadyResponder = readiness.GinHandler(ctx, probe)
	}

	router, err := sharedserver.NewRuntimeRouter(ctx, logger, routerOptions)
	if err != nil {
		return nil, fmt.Errorf("runtime router: %w", err)
	}

	router.NoRoute(middleware.NoRouteAuthHandler(appConfig.Server.APIKey))

	return router, nil
}
