package bot

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/iris"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/database"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/workerpool"
)

// BotLifecycle: 봇의 시작/준비대기/종료 수명주기를 담당합니다.
type BotLifecycle struct {
	logger      *slog.Logger
	cache       *cache.Service
	irisClient  iris.Client
	irisBaseURL string
	stopCh      chan struct{}
	doneCh      chan struct{}
	doneOnce    sync.Once
	workerPool  *workerpool.Pool
	holodex     streamRuntime
	postgres    *database.PostgresService
}

func NewBotLifecycle(
	logger *slog.Logger,
	cacheSvc *cache.Service,
	irisClient iris.Client,
	irisBaseURL string,
	stopCh chan struct{},
	doneCh chan struct{},
	workerPool *workerpool.Pool,
	holodex streamRuntime,
	postgres *database.PostgresService,
) *BotLifecycle {
	return &BotLifecycle{
		logger:      logger,
		cache:       cacheSvc,
		irisClient:  irisClient,
		irisBaseURL: irisBaseURL,
		stopCh:      stopCh,
		doneCh:      doneCh,
		workerPool:  workerPool,
		holodex:     holodex,
		postgres:    postgres,
	}
}

func (l *BotLifecycle) Start(ctx context.Context) error {
	l.logInfo("Starting Hololive KakaoTalk Bot...")

	if l.cache == nil {
		return fmt.Errorf("start bot: cache is not configured")
	}

	if err := l.cache.WaitUntilReady(ctx, constants.ValkeyConfig.ReadyTimeout); err != nil {
		return fmt.Errorf("start bot: valkey connection timeout: %w", err)
	}
	l.logInfo("Valkey connected")

	if err := l.WaitUntilIrisReady(
		ctx,
		constants.IrisConnection.ReadyTimeout,
		constants.IrisConnection.RetryInterval,
		constants.IrisConnection.PingTimeout,
	); err != nil {
		l.logWarn(
			"Iris server not ready at startup; continuing in degraded mode",
			slog.String("base_url", l.irisBaseURL),
			slog.Any("error", err),
		)
	} else {
		l.logInfo("Iris server connected")
	}

	l.logInfo("Bot started successfully")

	select {
	case <-ctx.Done():
		l.logInfo("Context canceled, shutting down...")
		return fmt.Errorf("start bot: context canceled: %w", ctx.Err())
	case <-l.stopCh:
		l.logInfo("Stop signal received")
		return nil
	}
}

func (l *BotLifecycle) WaitUntilIrisReady(
	ctx context.Context,
	timeout, retryInterval, pingTimeout time.Duration,
) error {
	if l == nil || l.irisClient == nil {
		return fmt.Errorf("wait for iris ready: iris client is not configured")
	}

	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(retryInterval)
	defer ticker.Stop()

	attempt := 0
	startedAt := time.Now()
	lastWarnLoggedAt := time.Time{}
	for {
		attempt++
		pingCtx, pingCancel := context.WithTimeout(waitCtx, pingTimeout)
		ready := l.irisClient.Ping(pingCtx)
		pingCancel()

		if ready {
			if attempt > 1 {
				l.logInfo(
					"Iris server became ready after retry",
					slog.Int("attempt", attempt),
					slog.Duration("elapsed", time.Since(startedAt)),
				)
			}
			return nil
		}

		now := time.Now()
		if attempt == 1 || lastWarnLoggedAt.IsZero() || now.Sub(lastWarnLoggedAt) >= time.Minute {
			l.logWarn(
				"Iris server not ready, retrying",
				slog.Int("attempt", attempt),
				slog.Duration("retry_interval", retryInterval),
				slog.Duration("elapsed", now.Sub(startedAt)),
			)
			lastWarnLoggedAt = now
		}

		select {
		case <-waitCtx.Done():
			if errors.Is(waitCtx.Err(), context.DeadlineExceeded) {
				return fmt.Errorf("wait for iris ready: timeout after %s", timeout)
			}
			return fmt.Errorf("wait for iris ready: canceled: %w", waitCtx.Err())
		case <-ticker.C:
		}
	}
}

func (l *BotLifecycle) Shutdown(ctx context.Context) error {
	l.logInfo("Shutting down bot...")

	if l.workerPool != nil {
		if err := l.workerPool.ShutdownWait(ctx); err != nil {
			l.logWarn("Worker pool shutdown error", slog.Any("error", err))
		}
	}

	if l.holodex != nil {
		l.holodex.Stop()
	}

	if l.cache != nil {
		if err := l.cache.Close(); err != nil {
			l.logWarn("Error closing cache", slog.Any("error", err))
		}
	}

	if l.postgres != nil {
		if err := l.postgres.Close(); err != nil {
			l.logWarn("Error closing postgres", slog.Any("error", err))
		}
	}

	l.doneOnce.Do(func() {
		if l.doneCh != nil {
			close(l.doneCh)
		}
	})

	l.logInfo("Bot shutdown complete")
	return nil
}

func (l *BotLifecycle) logInfo(msg string, attrs ...slog.Attr) {
	if l == nil || l.logger == nil {
		return
	}
	args := make([]any, 0, len(attrs))
	for _, attr := range attrs {
		args = append(args, attr)
	}
	l.logger.Info(msg, args...)
}

func (l *BotLifecycle) logWarn(msg string, attrs ...slog.Attr) {
	if l == nil || l.logger == nil {
		return
	}
	args := make([]any, 0, len(attrs))
	for _, attr := range attrs {
		args = append(args, attr)
	}
	l.logger.Warn(msg, args...)
}
