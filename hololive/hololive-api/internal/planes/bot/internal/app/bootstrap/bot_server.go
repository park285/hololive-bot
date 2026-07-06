package bootstrap

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/kapu/hololive-shared/pkg/config"
	sharedserver "github.com/kapu/hololive-shared/pkg/server"
	"github.com/park285/iris-client-go/webhook"
	sharedh3 "github.com/park285/shared-go/pkg/h3"
	"github.com/quic-go/quic-go/http3"

	apphttp "github.com/kapu/hololive-api/internal/planes/bot/internal/app/http"
	"github.com/kapu/hololive-api/internal/readiness"
)

func BuildBotServer(
	ctx context.Context,
	appConfig *config.Config,
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
	return sharedserver.NewH2CServer(addr, botRouter, "hololive-bot.http"), nil
}

func BuildBotHTTP3Server(
	ctx context.Context,
	appConfig *config.Config,
	webhookHandler *webhook.Handler,
	triggerHandler *sharedserver.TriggerHandler,
	irisRoomLister IrisRoomLister,
	logger *slog.Logger,
	readyProbe ...*readiness.Probe,
) (*http3.Server, func(context.Context), error) {
	return buildBotHTTP3ServerWithReloaderOptions(ctx, appConfig, webhookHandler, triggerHandler, irisRoomLister, logger, reloadingTLSCertificateOptions{}, readyProbe...)
}

func buildBotHTTP3ServerWithReloaderOptions(
	ctx context.Context,
	appConfig *config.Config,
	webhookHandler *webhook.Handler,
	triggerHandler *sharedserver.TriggerHandler,
	irisRoomLister IrisRoomLister,
	logger *slog.Logger,
	reloaderOptions reloadingTLSCertificateOptions,
	readyProbe ...*readiness.Probe,
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
