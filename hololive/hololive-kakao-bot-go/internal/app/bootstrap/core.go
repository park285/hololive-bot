package bootstrap

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/config"
	sharedmodules "github.com/kapu/hololive-shared/pkg/providers/modules"
)

func InitInfraResources(ctx context.Context, cfg *config.Config, logger *slog.Logger) (*sharedmodules.InfraModule, error) {
	module, err := sharedmodules.BuildInfraModule(ctx, cfg, logger)
	if err != nil {
		return nil, fmt.Errorf("provide infra resources: %w", err)
	}
	return module, nil
}
