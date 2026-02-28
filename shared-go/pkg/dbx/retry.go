package dbx

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// OpenWithRetry: exponential backoff로 PostgreSQL 연결을 재시도합니다.
// 스키마 마이그레이션이 완료되기 전 앱이 시작되는 Race Condition 방어용.
func OpenWithRetry(
	ctx context.Context,
	cfg Config,
	opt OpenOptions,
) (*Client, error) {
	retry := opt.Retry
	if retry.MaxAttempts <= 0 {
		retry.MaxAttempts = 5
	}
	if retry.BaseDelay <= 0 {
		retry.BaseDelay = 2 * time.Second
	}
	if retry.MaxDelay <= 0 {
		retry.MaxDelay = 30 * time.Second
	}

	logger := opt.Logger
	if logger == nil {
		logger = slog.Default()
	}

	var lastErr error
	for attempt := range retry.MaxAttempts {
		client, err := Open(ctx, cfg, opt)
		if err == nil {
			if attempt > 0 {
				logger.Info("postgres_connect_success_after_retry",
					slog.Int("attempts", attempt+1),
				)
			}
			return client, nil
		}

		lastErr = err

		// 마지막 시도면 재시도하지 않음
		if attempt >= retry.MaxAttempts-1 {
			break
		}

		// Exponential backoff: 2s, 4s, 8s, 16s, ... (최대 MaxDelay)
		delay := min(retry.BaseDelay*time.Duration(1<<uint(attempt)), retry.MaxDelay)

		logger.Warn("postgres_connect_retry",
			slog.Int("attempt", attempt+1),
			slog.Int("max_attempts", retry.MaxAttempts),
			slog.Duration("delay", delay),
			slog.Any("error", err),
		)

		// context 취소 확인 후 대기
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("postgres connect canceled: %w", ctx.Err())
		case <-time.After(delay):
		}
	}

	return nil, fmt.Errorf("postgres connect failed after %d attempts: %w", retry.MaxAttempts, lastErr)
}
