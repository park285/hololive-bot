package bootstrap

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/config"
	sharedmodules "github.com/kapu/hololive-shared/pkg/providers/modules"
)

func InitInfraResources(ctx context.Context, appConfig *config.Config, logger *slog.Logger) (*sharedmodules.InfraModule, error) {
	module, err := sharedmodules.BuildInfraModule(ctx, appConfig, logger)
	if err != nil {
		return nil, fmt.Errorf("provide infra resources: %w", err)
	}
	return module, nil
}
