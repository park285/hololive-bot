package workerapp

import (
	"context"
	"errors"
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/config"
)

func normalizeRuntimeBuildInputs(ctx context.Context, appConfig *config.Config, logger *slog.Logger) (context.Context, error) {
	if appConfig == nil {
		return nil, errors.New("config must not be nil")
	}
	if logger == nil {
		return nil, errors.New("logger must not be nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	return ctx, nil
}
