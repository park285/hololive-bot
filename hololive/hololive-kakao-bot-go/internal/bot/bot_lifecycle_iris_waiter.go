package bot

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
