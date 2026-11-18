package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/kapu/hololive-shared/pkg/constants"
)

const (
	irisReadyTimeout       = 10 * time.Minute
	irisReadyRetryInterval = 2 * time.Second
	irisReadyPingTimeout   = 3 * time.Second
)

type BotLifecycle struct {
	logger      *slog.Logger
	cache       CacheReadiness
	irisClient  IrisPinger
	irisBaseURL string
}

func NewBotLifecycle(logger *slog.Logger, cacheClient CacheReadiness, irisClient IrisPinger, irisBaseURL string) *BotLifecycle {
	return &BotLifecycle{
		logger:      logger,
		cache:       cacheClient,
		irisClient:  irisClient,
		irisBaseURL: irisBaseURL,
	}
}

func (l *BotLifecycle) Start(ctx context.Context) error {
	l.logInfo("Starting Hololive KakaoTalk Bot...")

	if l.cache == nil {
		return errors.New("start bot: cache is not configured")
	}

	if err := l.cache.WaitUntilReady(ctx, constants.ValkeyConfig.ReadyTimeout); err != nil {
		return fmt.Errorf("start bot: valkey connection timeout: %w", err)
	}

	l.logInfo("Valkey connected")

	if err := l.WaitUntilIrisReady(ctx, irisReadyTimeout, irisReadyRetryInterval, irisReadyPingTimeout); err != nil {
		l.logWarn(
			"Iris server not ready at startup; continuing, reply outbox retries delivery",
			slog.String("base_url", l.irisBaseURL),
			slog.Any("error", err),
		)
	} else {
		l.logInfo("Iris server connected")
	}

	l.logInfo("Bot started successfully")

	<-ctx.Done()
	l.logInfo("Context canceled, shutting down...")

	return fmt.Errorf("start bot: context canceled: %w", ctx.Err())
}

func (l *BotLifecycle) Shutdown(_ context.Context) error {
	l.logInfo("Shutting down bot...")
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
