package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"

	"github.com/kapu/hololive-shared/pkg/config/settings"
	"github.com/kapu/hololive-shared/pkg/service/cache"
)

type persistedStateAuditor interface {
	AuditPersistedState(context.Context) (cache.PersistedStateAudit, error)
	Close() error
}

type cacheConnector func(context.Context, cache.Config, *slog.Logger) (persistedStateAuditor, error)

func main() {
	if err := runMain(); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func runMain() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	return run(ctx, os.Stdout, settings.LoadValkeyConfig, connectCache)
}

func run(
	ctx context.Context,
	stdout io.Writer,
	loadConfig func() settings.ValkeyConfig,
	connect cacheConnector,
) (runErr error) {
	if loadConfig == nil || connect == nil {
		return fmt.Errorf("run read-only state audit: dependencies are nil")
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	config := loadConfig()
	service, err := connect(ctx, cache.Config{
		Host:              config.Host,
		Port:              config.Port,
		Password:          config.Password,
		DB:                config.DB,
		SocketPath:        config.SocketPath,
		DisableCache:      true,
		ForceSingleClient: true,
	}, logger)
	if err != nil {
		return fmt.Errorf("connect valkey for read-only state audit: %w", err)
	}
	return auditAndEncode(ctx, stdout, service)
}

func auditAndEncode(
	ctx context.Context,
	stdout io.Writer,
	service persistedStateAuditor,
) (runErr error) {
	defer func() {
		if err := service.Close(); err != nil {
			runErr = errors.Join(runErr, fmt.Errorf("close valkey state audit connection: %w", err))
		}
	}()

	report, err := service.AuditPersistedState(ctx)
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(stdout)
	if err := encoder.Encode(report); err != nil {
		return fmt.Errorf("encode count-only state audit: %w", err)
	}
	return nil
}

func connectCache(ctx context.Context, config cache.Config, logger *slog.Logger) (persistedStateAuditor, error) {
	return cache.NewCacheService(ctx, config, logger)
}
