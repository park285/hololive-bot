package app

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/kapu/hololive-shared/pkg/constants"
)

// Run: 봇 애플리케이션을 실행하고 종료 신호(SIGINT, SIGTERM)를 대기한다. (블로킹)
func (r *BotRuntime) Run() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	errCh := make(chan error, 1)
	r.Start(ctx, errCh)
	r.Logger.Info("Bot started, waiting for signals...")

	select {
	case sig := <-sigCh:
		r.Logger.Info("Received shutdown signal", slog.String("signal", sig.String()))
	case err := <-errCh:
		r.Logger.Error("Server error", slog.Any("error", err))
	}

	r.Logger.Info("Shutting down gracefully...")
	cancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), constants.AppTimeout.Shutdown)
	defer shutdownCancel()

	r.Shutdown(shutdownCtx)

	r.Logger.Info("Shutdown complete")
}
