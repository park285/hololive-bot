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
	readyProbe *sharedreadiness.Probe,
) (*gin.Engine, error) {
	if err := validateAPIRouterInputs(appConfig, domainHandlers); err != nil {
		return nil, fmt.Errorf("validate API router inputs: %w", err)
	}

	if logger == nil {
		logger = slog.Default()
	}

	router, err := newAPIRouter(ctx, appConfig, logger, readyProbe)
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

func newAPIRouter(ctx context.Context, appConfig *apiconfig.AdminPlaneConfig, logger *slog.Logger, readyProbe *sharedreadiness.Probe) (*gin.Engine, error) {
	if appConfig == nil {
		return nil, errors.New("config must not be nil")
	}

	if readyProbe == nil {
		return nil, errors.New("ready probe must not be nil")
	}

	if logger == nil {
		logger = slog.Default()
	}

	routerOptions := &sharedserver.RuntimeRouterOptions{
		APIKey:                 appConfig.Server.APIKey,
		EnableGzip:             true,
		SkipLogPaths:           []string{"/metrics"},
		TrustRemoteAddrOnly:    true,
		InternalReadyResponder: readiness.GinHandler(ctx, readyProbe),
	}

	router, err := sharedserver.NewRuntimeRouter(ctx, logger, routerOptions)
	if err != nil {
		return nil, fmt.Errorf("runtime router: %w", err)
	}

	router.NoRoute(middleware.NoRouteAuthHandler(appConfig.Server.APIKey))

	return router, nil
}
