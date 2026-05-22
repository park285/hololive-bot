package bootstrap

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/kapu/hololive-shared/pkg/config"
	sharedserver "github.com/kapu/hololive-shared/pkg/server"
	"github.com/park285/iris-client-go/iris"
	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/http3"

	apphttp "github.com/kapu/hololive-kakao-bot-go/internal/app/http"
)

func BuildBotServer(
	ctx context.Context,
	appConfig *config.Config,
	webhookHandler *iris.WebhookHandler,
	triggerHandler *sharedserver.TriggerHandler,
	logger *slog.Logger,
) (*http.Server, error) {
	botRouter, err := apphttp.ProvideBotRouter(ctx, appConfig, logger, webhookHandler, triggerHandler)
	if err != nil {
		return nil, fmt.Errorf("build bot server: provide bot router: %w", err)
	}

	addr := appConfig.Server.H2CAddr
	if addr == "" {
		addr = fmt.Sprintf(":%d", appConfig.Server.Port)
	}
	return sharedserver.NewH2CServer(addr, botRouter, "hololive-bot.http"), nil
}

func BuildBotHTTP3Server(
	ctx context.Context,
	appConfig *config.Config,
	webhookHandler *iris.WebhookHandler,
	triggerHandler *sharedserver.TriggerHandler,
	logger *slog.Logger,
) (*http3.Server, error) {
	botRouter, err := apphttp.ProvideBotRouter(ctx, appConfig, logger, webhookHandler, triggerHandler)
	if err != nil {
		return nil, fmt.Errorf("build bot h3 server: provide bot router: %w", err)
	}

	cert, err := tls.LoadX509KeyPair(appConfig.Server.H3CertFile, appConfig.Server.H3KeyFile)
	if err != nil {
		return nil, fmt.Errorf("load h3 certificate: %w", err)
	}

	quicConfig := &quic.Config{
		InitialPacketSize: 1200,
		KeepAlivePeriod:   10 * time.Second,
		MaxIdleTimeout:    60 * time.Second,
	}

	return &http3.Server{
		Addr:    appConfig.Server.H3Addr,
		Handler: botRouter,
		TLSConfig: http3.ConfigureTLSConfig(&tls.Config{
			MinVersion:   tls.VersionTLS13,
			Certificates: []tls.Certificate{cert},
		}),
		QUICConfig:     quicConfig,
		MaxHeaderBytes: http.DefaultMaxHeaderBytes,
	}, nil
}
