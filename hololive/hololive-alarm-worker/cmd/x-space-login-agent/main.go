package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kapu/hololive-alarm-worker/internal/service/xspaces"
	sessions "github.com/kapu/hololive-shared/pkg/service/xspaces"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	if err := run(ctx, logger); err != nil {
		// 오류 원문은 드라이버·브라우저·입력 값을 포함할 수 있습니다.
		logger.Error("X login agent stopped; inspect recovery status")
		os.Exit(1)
	}
}

func run(ctx context.Context, logger *slog.Logger) error {
	path := os.Getenv("X_SPACES_LOGIN_FILE")
	if path == "" {
		logger.Info("X login agent disabled")

		return nil
	}

	if os.Getenv("PGSSLMODE") != "verify-full" {
		return errors.New("x login database requires verify-full")
	}

	config, err := pgxpool.ParseConfig("")
	if err != nil {
		return errors.New("invalid X login database configuration")
	}

	config.MaxConns, config.MinConns = 1, 0
	config.ConnConfig.ConnectTimeout = 10 * time.Second

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return errors.New("open X login database failed")
	}
	defer pool.Close()

	store, err := sessions.LoadStore(pool)
	if err != nil {
		return fmt.Errorf("load X session store: %w", err)
	}

	return runLoginLoop(ctx, path, store, logger)
}

func runLoginLoop(ctx context.Context, path string, store *sessions.Store, logger *slog.Logger) error {
	for ctx.Err() == nil {
		if err := cycle(ctx, path, store, logger); err != nil {
			return err
		}

		if err := os.WriteFile("/run/x-space-login/heartbeat", []byte("ok\n"), 0o600); err != nil {
			return errors.New("write X login heartbeat failed")
		}

		select {
		case <-ctx.Done():
			return nil
		case <-time.After(time.Minute):
		}
	}

	return nil
}

func cycle(ctx context.Context, path string, store *sessions.Store, logger *slog.Logger) error {
	config, err := xspaces.LoadLoginConfig(path)
	if err != nil {
		return fmt.Errorf("load X login configuration: %w", err)
	}

	defer func() { config.Password = "" }()

	queryCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	if err = store.ReconcileLogin(queryCtx); err != nil {
		cancel()

		return fmt.Errorf("reconcile X login: %w", err)
	}

	id, err := store.ClaimLogin(queryCtx, config.Revision)

	cancel()

	if err != nil {
		return fmt.Errorf("claim X login: %w", err)
	}

	if id == 0 {
		return nil
	}

	logger.Info("X login attempt started", slog.Int64("attempt", id))

	result, fatal := xspaces.ProcessLogin(ctx, config)
	finishCtx, finishCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)

	defer finishCancel()

	if err := store.FinishLogin(finishCtx, id, result.Cookies, result.Error); err != nil {
		return fmt.Errorf("finish X login: %w", err)
	}

	logger.Info("X login attempt recorded", slog.Int64("attempt", id), slog.String("code", result.Error))

	if fatal {
		return errors.New("x browser process requires container cleanup")
	}

	return nil
}
