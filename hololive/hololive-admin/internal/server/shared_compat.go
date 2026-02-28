package server

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/kapu/hololive-shared/pkg/domain"
	sharedserver "github.com/kapu/hololive-shared/pkg/server"
	"github.com/kapu/hololive-shared/pkg/service/holodex"
	"github.com/kapu/hololive-shared/pkg/service/youtube"
)

const (
	APIKeyHeader = sharedserver.APIKeyHeader
)

var (
	wsUpgrader = sharedserver.WSUpgrader
)

func WrapH2C(handler http.Handler) http.Handler {
	return sharedserver.WrapH2C(handler)
}

func APIKeyAuthMiddleware(apiKey string) gin.HandlerFunc {
	return sharedserver.APIKeyAuthMiddleware(apiKey)
}

func NoRouteAuthHandler(apiKey string) gin.HandlerFunc {
	return sharedserver.NoRouteAuthHandler(apiKey)
}

func LoggerMiddleware(ctx context.Context, logger *slog.Logger, skipPaths ...string) gin.HandlerFunc {
	return sharedserver.LoggerMiddleware(ctx, logger, skipPaths...)
}

func SecurityHeadersMiddleware() gin.HandlerFunc {
	return sharedserver.SecurityHeadersMiddleware()
}

func ClientHintsMiddleware() func(c *gin.Context) {
	return sharedserver.ClientHintsMiddleware()
}

func NewIPAllowList(allowed []string) ([]*net.IPNet, error) {
	nets, err := sharedserver.NewIPAllowList(allowed)
	if err != nil {
		return nil, fmt.Errorf("new IP allow list: %w", err)
	}
	return nets, nil
}

func AdminIPAllowMiddleware(allowed []*net.IPNet, logger *slog.Logger) gin.HandlerFunc {
	return sharedserver.AdminIPAllowMiddleware(allowed, logger)
}

func NewLocalSettingsApplier(
	youtubeSvc *youtube.Service,
	holodexSvc *holodex.Service,
	scraperProxyToggler ScraperProxyToggler,
	alarm domain.AlarmCRUD,
) SettingsApplier {
	return sharedserver.NewLocalSettingsApplier(youtubeSvc, holodexSvc, scraperProxyToggler, alarm)
}
