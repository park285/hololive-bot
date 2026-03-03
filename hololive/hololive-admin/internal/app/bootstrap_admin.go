package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/constants"
	providers "github.com/kapu/hololive-shared/pkg/providers"
)

// AdminAPIRuntime: admin-api 전용 런타임
type AdminAPIRuntime struct {
	Config     *config.AdminAPIConfig
	Logger     *slog.Logger
	cleanup    func()
	httpServer *http.Server
}

// Close: 리소스를 정리합니다.
func (r *AdminAPIRuntime) Close() {
	if r != nil && r.cleanup != nil {
		r.cleanup()
	}
}

// Run: SIGINT/SIGTERM 신호를 대기하며 graceful shutdown을 수행합니다. (블로킹)
func (r *AdminAPIRuntime) Run() {
	if r == nil {
		return
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	errCh := make(chan error, 1)

	// HTTP 서버 시작
	go func() {
		if err := r.httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("HTTP server error: %w", err)
		}
	}()
	r.Logger.Info("Admin API HTTP server started",
		slog.String("addr", r.httpServer.Addr))

	select {
	case sig := <-sigCh:
		r.Logger.Info("Received shutdown signal", slog.String("signal", sig.String()))
	case err := <-errCh:
		r.Logger.Error("Server error", slog.Any("error", err))
	}

	r.Logger.Info("Shutting down gracefully...")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), constants.AppTimeout.Shutdown)
	defer shutdownCancel()

	if err := r.httpServer.Shutdown(shutdownCtx); err != nil {
		r.Logger.Error("HTTP server shutdown error", slog.Any("error", err))
	} else {
		r.Logger.Info("HTTP server stopped")
	}

	r.Logger.Info("Shutdown complete")
}

// BuildAdminAPIRuntime: admin-api 런타임을 구성합니다.
func BuildAdminAPIRuntime(ctx context.Context, cfg *config.AdminAPIConfig, logger *slog.Logger) (*AdminAPIRuntime, error) {
	if cfg == nil {
		return nil, fmt.Errorf("admin api config must not be nil")
	}
	if logger == nil {
		return nil, fmt.Errorf("logger must not be nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	// 1. Valkey 캐시 초기화
	cacheResources, cleanupCache, err := providers.ProvideCacheResources(ctx, cfg.Valkey, logger)
	if err != nil {
		return nil, fmt.Errorf("init cache: %w", err)
	}
	cacheService := providers.ProvideCacheService(cacheResources)

	// 2. PostgreSQL 초기화
	databaseResources, cleanupDB, err := providers.ProvideDatabaseResources(ctx, cfg.Postgres, logger)
	if err != nil {
		cleanupCache()
		return nil, fmt.Errorf("init database: %w", err)
	}
	postgresService := providers.ProvidePostgresService(databaseResources)

	cleanup := func() {
		cleanupDB()
		cleanupCache()
	}

	runtime, err := buildAdminComponents(ctx, cfg, logger, cacheService, postgresService, cleanup)
	if err != nil {
		cleanup()
		return nil, err
	}

	return runtime, nil
}
