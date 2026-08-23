package bootstrap

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/kapu/hololive-shared/pkg/config/settings"

	sharedserver "github.com/kapu/hololive-shared/pkg/server/httpserver"
	"github.com/park285/iris-client-go/v2/webhook"
	sharedh3 "github.com/park285/shared-go/v2/pkg/h3"
	"github.com/quic-go/quic-go/http3"

	apphttp "github.com/kapu/hololive-api/internal/planes/bot/internal/app/http"
	sharedreadiness "github.com/kapu/hololive-shared/pkg/readiness"
)

func BuildBotServer(
	ctx context.Context,
	appConfig *settings.Config,
	webhookHandler *webhook.Handler,
	triggerHandler *sharedserver.TriggerHandler,
	irisRoomLister IrisRoomLister,
	logger *slog.Logger,
) (*http.Server, error) {
	botRouter, err := apphttp.ProvideBotRouter(ctx, appConfig, logger, webhookHandler, triggerHandler, irisRoomLister)
	if err != nil {
		return nil, fmt.Errorf("build bot server: provide bot router: %w", err)
	}

	addr := fmt.Sprintf(":%d", appConfig.Server.Port)
	return sharedserver.NewH2CServer(addr, botRouter, "hololive-bot.http", sharedserver.LocalPlaneTraceFilter), nil
}

func BuildShortLinkServer(addr string) *http.Server {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return nil
	}
	return sharedserver.NewH2CServer(addr, apphttp.ProvideShortLinkHandler(), "hololive-bot.shortlink",
		sharedserver.LocalPlaneTraceFilter)
}

func BuildBotHTTP3Server(
	ctx context.Context,
	appConfig *settings.Config,
	webhookHandler *webhook.Handler,
	triggerHandler *sharedserver.TriggerHandler,
	irisRoomLister IrisRoomLister,
	logger *slog.Logger,
	readyProbe ...*sharedreadiness.Probe,
) (*http3.Server, func(context.Context), error) {
	return buildBotHTTP3ServerWithReloaderOptions(ctx, appConfig, webhookHandler, triggerHandler, irisRoomLister, logger, reloadingTLSCertificateOptions{}, readyProbe...)
}

func buildBotHTTP3ServerWithReloaderOptions(
	ctx context.Context,
	appConfig *settings.Config,
	webhookHandler *webhook.Handler,
	triggerHandler *sharedserver.TriggerHandler,
	irisRoomLister IrisRoomLister,
	logger *slog.Logger,
	reloaderOptions reloadingTLSCertificateOptions,
	readyProbe ...*sharedreadiness.Probe,
) (*http3.Server, func(context.Context), error) {
	botRouter, err := apphttp.ProvideBotRouter(ctx, appConfig, logger, webhookHandler, triggerHandler, irisRoomLister, readyProbe...)
	if err != nil {
		return nil, nil, fmt.Errorf("build bot h3 server: provide bot router: %w", err)
	}

	certReloader, err := newReloadingTLSCertificateWithOptions(appConfig.Server.H3CertFile, appConfig.Server.H3KeyFile, logger, reloaderOptions)
	if err != nil {
		return nil, nil, fmt.Errorf("load h3 certificate: %w", err)
	}

	tlsConfig := &tls.Config{
		MinVersion:     tls.VersionTLS13,
		GetCertificate: certReloader.GetCertificate,
	}

	return sharedh3.NewServerWithTLSConfig(appConfig.Server.H3Addr, botRouter, tlsConfig), certReloader.Start, nil
}
