package botruntime

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/quic-go/quic-go/http3"

	apiconfig "github.com/kapu/hololive-api/internal/config"
	appbootstrap "github.com/kapu/hololive-api/internal/planes/bot/internal/app/bootstrap"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/bot/orchestration"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/service/broadcasttypedecision"
	"github.com/kapu/hololive-api/internal/service/geminidecision"
	sharedreadiness "github.com/kapu/hololive-shared/pkg/readiness"
	sharedserver "github.com/kapu/hololive-shared/pkg/server/httpserver"
)

func newBotReadyProbe(postgres, durable sharedreadiness.Pinger, cacheClient sharedreadiness.ConnectionChecker) *sharedreadiness.Probe {
	return sharedreadiness.NewProbe("bot",
		sharedreadiness.PostgresCheck(postgres),
		sharedreadiness.Check{Name: "durable_store", Probe: durable.Ping},
		sharedreadiness.ValkeyCheck(cacheClient),
	)
}

func buildBotOptionalServers(ctx context.Context, appConfig *apiconfig.BotPlaneConfig) (metricsServer, pprofServer *http.Server) {
	if metricsAddr := strings.TrimSpace(appConfig.Server.MetricsAddr); metricsAddr != "" {
		metricsServer = sharedserver.NewMetricsServer(ctx, metricsAddr, appConfig.Server.APIKey, nil)
	}

	if pprofAddr := strings.TrimSpace(appConfig.Server.PprofAddr); pprofAddr != "" {
		pprofServer = sharedserver.NewPprofServer(ctx, pprofAddr, appConfig.Server.APIKey)
	}

	return metricsServer, pprofServer
}

func buildBotRuntime(ctx context.Context, appConfig *apiconfig.BotPlaneConfig, logger *slog.Logger, infra *appbootstrap.BotInfrastructure) (_ *BotRuntime, retErr error) {
	if appConfig == nil {
		return nil, errors.New("app config is nil")
	}

	if infra == nil {
		return nil, errors.New("infra is nil")
	}

	if infra.Durable == nil {
		return nil, errors.New("durable store is nil")
	}

	outbox, err := newDurableReplyOutbox(infra.Durable, appConfig.APIWorkerProfile)
	if err != nil {
		return nil, fmt.Errorf("build reply outbox: %w", err)
	}

	infra.Deps.ReplyOutbox = outbox.writer(logger)

	botBot, err := orchestration.NewBot(infra.Deps)
	if err != nil {
		return nil, fmt.Errorf("create bot: %w", err)
	}

	durable, err := newDurableRuntime(botBot, infra.Deps.Client, infra.Durable, outbox, appConfig.APIWorkerProfile, logger)
	if err != nil {
		return nil, fmt.Errorf("build durable runtime: %w", err)
	}

	webhookHandler, err := appbootstrap.BuildDurableBotWebhookHandler(appConfig, durableAdmitter{
		inbox:  durable.inbox,
		wake:   func() { notifyDurable(durable.inboxWake) },
		accept: botBot.AcceptsMessage,
		logger: logger,
		totals: durable.inboxTotals,
	}, infra.Durable, logger)
	if err != nil {
		return nil, fmt.Errorf("build webhook handler: %w", err)
	}

	defer func() {
		if retErr != nil {
			retErr = errors.Join(retErr, webhookHandler.CloseContext(ctx))
		}
	}()

	readyProbe := newBotReadyProbe(infra.Postgres, infra.Durable, infra.Cache)

	var (
		h3Server          *http3.Server
		h3CertReloadStart func(context.Context)
	)

	if appConfig.Server.TransportEnabled("h3") {
		h3Server, h3CertReloadStart, err = appbootstrap.BuildBotHTTP3Server(ctx, appConfig, webhookHandler, logger, readyProbe)
		if err != nil {
			return nil, fmt.Errorf("build bot HTTP3 server: %w", err)
		}
	}

	metricsServer, pprofServer := buildBotOptionalServers(ctx, appConfig)

	return assembleBotRuntime(appConfig, logger, botBot, h3Server, h3CertReloadStart, metricsServer, pprofServer, webhookHandler, durable), nil
}

func newBroadcastTypeDecisionWorker(keyFile string, infra *appbootstrap.BotInfrastructure, logger *slog.Logger) (*broadcasttypedecision.Worker, error) {
	if infra.Postgres == nil || infra.Postgres.GetPool() == nil {
		return nil, errors.New("broadcast type decision postgres pool is nil")
	}

	key, err := geminidecision.LoadAPIKey(keyFile)
	if err != nil {
		return nil, fmt.Errorf("load broadcast type gemini api key: %w", err)
	}

	return broadcasttypedecision.New(infra.Postgres.GetPool(), key, logger), nil
}

func assembleBotRuntime(
	appConfig *apiconfig.BotPlaneConfig,
	logger *slog.Logger,
	botBot *orchestration.Bot,
	h3Server *http3.Server,
	h3CertReloadStart func(context.Context),
	metricsServer, pprofServer *http.Server,
	webhookHandler interface{ CloseContext(context.Context) error },
	durable *durableRuntime,
) *BotRuntime {
	return &BotRuntime{
		Config:               appConfig,
		Logger:               logger,
		Bot:                  botBot,
		ServerAddr:           appConfig.Server.H3Addr,
		H3Server:             h3Server,
		ShortLinkServer:      appbootstrap.BuildShortLinkServer(appConfig.Server.ShortLinkAddr),
		MetricsServer:        metricsServer,
		PprofServer:          pprofServer,
		h3CertReloadStart:    h3CertReloadStart,
		webhookHandlerCloser: webhookHandler,
		durable:              durable,
	}
}
