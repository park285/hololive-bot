package bootstrap

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/config/settings"
	sharedmodules "github.com/kapu/hololive-shared/pkg/providers/modules"
)

func InitInfraResources(ctx context.Context, appConfig *settings.Config, logger *slog.Logger) (*sharedmodules.InfraModule, error) {
	module, err := sharedmodules.BuildInfraModule(ctx, sharedmodules.InfraOptions{Valkey: appConfig.Valkey, Postgres: appConfig.Postgres}, logger)
	if err != nil {
		return nil, fmt.Errorf("provide infra resources: %w", err)
	}

	return module, nil
}
