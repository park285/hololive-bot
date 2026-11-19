package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

func (l *BotLifecycle) WaitUntilIrisReady(
	ctx context.Context,
	timeout, retryInterval, pingTimeout time.Duration,
) error {
	if err := l.validateIrisReadyWaiter(); err != nil {
		return fmt.Errorf("validate iris ready waiter: %w", err)
	}

	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(retryInterval)
	defer ticker.Stop()

	if err := l.runIrisReadyWaitLoop(waitCtx, ticker.C, timeout, retryInterval, pingTimeout); err != nil {
		return fmt.Errorf("run iris ready wait loop: %w", err)
	}

	return nil
}

func (l *BotLifecycle) validateIrisReadyWaiter() error {
	if l == nil || l.irisClient == nil {
		return errors.New("wait for iris ready: iris client is not configured")
	}

	return nil
}

func (l *BotLifecycle) runIrisReadyWaitLoop(
	waitCtx context.Context,
	tick <-chan time.Time,
	timeout, retryInterval, pingTimeout time.Duration,
) error {
	attempt := 0
	startedAt := time.Now()
	lastWarnLoggedAt := time.Time{}

	for {
		attempt++

		pingErr := l.pingIrisReady(waitCtx, pingTimeout)
		if pingErr == nil {
			l.logIrisReadyAfterRetry(attempt, startedAt)

			return nil
		}

		if loggedAt, ok := l.logIrisNotReadyRetry(attempt, retryInterval, startedAt, lastWarnLoggedAt, pingErr); ok {
			lastWarnLoggedAt = loggedAt
		}

		if err := waitNextIrisReadyRetry(waitCtx, tick, timeout); err != nil {
			return fmt.Errorf("wait next iris ready retry: %w", err)
		}
	}
}

func (l *BotLifecycle) pingIrisReady(ctx context.Context, pingTimeout time.Duration) error {
	pingCtx, pingCancel := context.WithTimeout(ctx, pingTimeout)
	defer pingCancel()

	if err := l.irisClient.Ping(pingCtx); err != nil {
		return fmt.Errorf("ping iris: %w", err)
	}

	return nil
}

func (l *BotLifecycle) logIrisReadyAfterRetry(attempt int, startedAt time.Time) {
	if attempt <= 1 {
		return
	}

	l.logInfo(
		"Iris server became ready after retry",
		slog.Int("attempt", attempt),
		slog.Duration("elapsed", time.Since(startedAt)),
	)
}

func (l *BotLifecycle) logIrisNotReadyRetry(attempt int, retryInterval time.Duration, startedAt, lastWarnLoggedAt time.Time, pingErr error) (time.Time, bool) {
	now := time.Now()
	if attempt != 1 && !lastWarnLoggedAt.IsZero() && now.Sub(lastWarnLoggedAt) < time.Minute {
		return lastWarnLoggedAt, false
	}

	l.logWarn(
		"Iris server not ready, retrying",
		slog.Int("attempt", attempt),
		slog.Duration("retry_interval", retryInterval),
		slog.Duration("elapsed", now.Sub(startedAt)),
		slog.Any("error", pingErr),
	)

	return now, true
}

func waitNextIrisReadyRetry(ctx context.Context, tick <-chan time.Time, timeout time.Duration) error {
	select {
	case <-ctx.Done():
		return fmt.Errorf("iris ready wait err: %w", irisReadyWaitErr(ctx.Err(), timeout))
	case <-tick:
		return nil
	}
}

func irisReadyWaitErr(err error, timeout time.Duration) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("wait for iris ready: timeout after %s", timeout)
	}

	return fmt.Errorf("wait for iris ready: canceled: %w", err)
}
