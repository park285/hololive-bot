package app

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/config"
	sharedserver "github.com/kapu/hololive-shared/pkg/server"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller"

	"github.com/kapu/hololive-kakao-bot-go/internal/server"
)

type botAdminServerDependencies struct {
	domainHandlers *server.DomainAPIHandlers
	authHandler    *server.AuthHandler
}

func buildBotAdminServerDependencies(
	ctx context.Context,
	cfg *config.Config,
	deps botAdminRuntimeDependencies,
	scraperScheduler *poller.Scheduler,
	logger *slog.Logger,
) (*botAdminServerDependencies, error) {
	if cfg == nil {
		return nil, fmt.Errorf("build bot admin server dependencies: config is nil")
	}
	if deps.cache == nil || deps.postgres == nil {
		return nil, fmt.Errorf("build bot admin server dependencies: admin dependency view is incomplete")
	}

	authService, err := buildBotAdminAuthService(ctx, cfg, deps, logger)
	if err != nil {
		return nil, fmt.Errorf("build bot admin server dependencies: %w", err)
	}

	settingsComponents := buildBotAdminSettingsComponents(cfg, deps, scraperScheduler, logger)
	systemCollector := buildBotAdminSystemCollector(cfg)
	domainHandlers := buildBotAdminAPIHandlers(
		deps,
		scraperScheduler,
		settingsComponents.settingsApplier,
		settingsComponents.majorEventTriggerClient,
		systemCollector,
		logger,
	)

	return &botAdminServerDependencies{
		domainHandlers: domainHandlers,
		authHandler:    server.NewAuthHandler(authService, logger),
	}, nil
}
