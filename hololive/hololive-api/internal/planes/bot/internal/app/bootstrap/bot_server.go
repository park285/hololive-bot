package bootstrap

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/park285/iris-client-go/v4/webhook"
	sharedh3 "github.com/park285/shared-go/v2/pkg/h3"
	"github.com/quic-go/quic-go/http3"

	apiconfig "github.com/kapu/hololive-api/internal/config"
	apiserver "github.com/kapu/hololive-api/internal/httpapi"
	apphttp "github.com/kapu/hololive-api/internal/planes/bot/internal/app/http"
	sharedreadiness "github.com/kapu/hololive-shared/pkg/readiness"
	sharedserver "github.com/kapu/hololive-shared/pkg/server/httpserver"
)

func BuildShortLinkServer(addr string) *http.Server {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return nil
	}

	return sharedserver.NewHTTPServer(addr, apphttp.ProvideShortLinkHandler(), "hololive-bot.shortlink",
		sharedserver.LocalPlaneTraceFilter)
}

func BuildBotHTTP3Server(
	ctx context.Context,
	appConfig *apiconfig.BotPlaneConfig,
	webhookHandler *webhook.Handler,
	triggerHandler *apiserver.TriggerHandler,
	irisRoomLister IrisRoomLister,
	logger *slog.Logger,
	readyProbe ...*sharedreadiness.Probe,
) (*http3.Server, func(context.Context), error) {
	server, startReloader, err := buildBotHTTP3ServerWithReloaderOptions(ctx, appConfig, webhookHandler, triggerHandler, irisRoomLister, logger, sharedh3.CertificateReloaderOptions{}, readyProbe...)
	if err != nil {
		return nil, nil, fmt.Errorf("build bot HTTP3 server with reloader options: %w", err)
	}

	return server, startReloader, nil
}

func buildBotHTTP3ServerWithReloaderOptions(
	ctx context.Context,
	appConfig *apiconfig.BotPlaneConfig,
	webhookHandler *webhook.Handler,
	triggerHandler *apiserver.TriggerHandler,
	irisRoomLister IrisRoomLister,
	logger *slog.Logger,
	reloaderOptions sharedh3.CertificateReloaderOptions,
	readyProbe ...*sharedreadiness.Probe,
) (*http3.Server, func(context.Context), error) {
	botRouter, err := apphttp.ProvideBotRouter(ctx, appConfig, logger, webhookHandler, triggerHandler, irisRoomLister, readyProbe...)
	if err != nil {
		return nil, nil, fmt.Errorf("build bot h3 server: provide bot router: %w", err)
	}

	reloaderOptions.Logger = logger

	certReloader, err := sharedh3.NewCertificateReloader(appConfig.Server.H3CertFile, appConfig.Server.H3KeyFile, reloaderOptions)
	if err != nil {
		return nil, nil, fmt.Errorf("load h3 certificate: %w", err)
	}

	tlsConfig := &tls.Config{
		MinVersion:     tls.VersionTLS13,
		GetCertificate: certReloader.GetCertificate,
	}

	// Run은 넘긴 ctx가 끝날 때까지 블로킹하므로 bot runtime이 tasksWG와 panicguard로 수명을 소유한다.
	// 실패는 reloader가 logger로 중복 없이 남긴다.
	return sharedh3.NewServerWithTLSConfig(appConfig.Server.H3Addr, botRouter, tlsConfig), certReloader.Run, nil
}
