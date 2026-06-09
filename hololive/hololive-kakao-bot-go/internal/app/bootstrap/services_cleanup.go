package bootstrap

import (
	"log/slog"
	"sync"
)

type irisCleanupCloser interface {
	Close() error
}

func composeBotInfrastructureCleanup(infraCleanup func(), irisClient any, logger *slog.Logger) func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			closeIrisClientForCleanup(irisClient, logger)
			if infraCleanup != nil {
				infraCleanup()
			}
		})
	}
}

func closeIrisClientForCleanup(irisClient any, logger *slog.Logger) {
	closer, ok := irisClient.(irisCleanupCloser)
	if !ok || closer == nil {
		return
	}

	if err := closer.Close(); err != nil && logger != nil {
		logger.Warn("iris_client_close_failed", slog.Any("error", err))
	}
}
