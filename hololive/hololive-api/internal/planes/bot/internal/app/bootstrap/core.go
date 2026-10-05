package bootstrap

import (
	"context"
	"fmt"
	"log/slog"

	apiconfig "github.com/kapu/hololive-api/internal/config"
	sharedmodules "github.com/kapu/hololive-shared/pkg/providers/modules"
)

func InitInfraResources(ctx context.Context, appConfig *apiconfig.BotPlaneConfig, logger *slog.Logger) (*sharedmodules.InfraModule, error) {
	module, err := sharedmodules.BuildInfraModule(ctx, sharedmodules.InfraOptions{Valkey: appConfig.Valkey, Postgres: appConfig.Postgres}, logger)
	if err != nil {
		return nil, fmt.Errorf("provide infra resources: %w", err)
	}

	return module, nil
}
