package bootstrap

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/kapu/hololive-shared/pkg/config"
	sharedserver "github.com/kapu/hololive-shared/pkg/server"
	"github.com/park285/iris-client-go/iris"

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

	addr := fmt.Sprintf(":%d", cfg.Server.Port)
	return sharedserver.NewH2CServer(addr, botRouter, "hololive-bot.http"), nil
}
