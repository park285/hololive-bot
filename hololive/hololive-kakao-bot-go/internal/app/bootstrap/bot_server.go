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
	cfg *config.Config,
	webhookHandler *iris.WebhookHandler,
	triggerHandler *sharedserver.TriggerHandler,
	logger *slog.Logger,
) (*http.Server, error) {
	botRouter, err := apphttp.ProvideBotRouter(ctx, cfg, logger, webhookHandler, triggerHandler)
	if err != nil {
		return nil, fmt.Errorf("build bot server: provide bot router: %w", err)
	}

	addr := cfg.Server.H2CAddr
	if addr == "" {
		addr = fmt.Sprintf(":%d", cfg.Server.Port)
	}
	return sharedserver.NewH2CServer(addr, botRouter, "hololive-bot.http"), nil
}

func BuildBotHTTP3Server(
	ctx context.Context,
	cfg *config.Config,
	webhookHandler *iris.WebhookHandler,
	triggerHandler *sharedserver.TriggerHandler,
	logger *slog.Logger,
) (*http3.Server, error) {
	botRouter, err := apphttp.ProvideBotRouter(ctx, cfg, logger, webhookHandler, triggerHandler)
	if err != nil {
		return nil, fmt.Errorf("build bot h3 server: provide bot router: %w", err)
	}

	cert, err := tls.LoadX509KeyPair(cfg.Server.H3CertFile, cfg.Server.H3KeyFile)
	if err != nil {
		return nil, fmt.Errorf("load h3 certificate: %w", err)
	}

	return &http3.Server{
		Addr:    cfg.Server.H3Addr,
		Handler: botRouter,
		TLSConfig: &tls.Config{
			MinVersion:   tls.VersionTLS13,
			Certificates: []tls.Certificate{cert},
		},
		QUICConfig: &quic.Config{
			KeepAlivePeriod:         10 * time.Second,
			MaxIdleTimeout:          60 * time.Second,
			InitialPacketSize:       1200,
			DisablePathMTUDiscovery: true,
		},
		MaxHeaderBytes: http.DefaultMaxHeaderBytes,
	}, nil
}
