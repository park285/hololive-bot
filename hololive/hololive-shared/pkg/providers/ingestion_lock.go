package providers

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/park285/llm-kakao-bots/shared-go/pkg/retry"

	"github.com/kapu/hololive-shared/pkg/service/cache"
)

const (
	// IngestionLeaseKey - 분산 락 키 (외부 서비스에서 로그 참조용)
	IngestionLeaseKey      = "lock:ingestion:runtime"
	ingestionLeaseTTL      = 2 * time.Minute
	ingestionLeaseRenewGap = 40 * time.Second
)

var errIngestionLeaseOwnershipLost = errors.New("ingestion lease ownership lost")

// IngestionLease - 분산 락 기반 ingestion 리스 (단일 인스턴스 보장)
type IngestionLease struct {
	cacheSvc      cache.Client
	key           string
	owner         string
	role          string
	ttl           time.Duration
	renewInterval time.Duration
	logger        *slog.Logger
	// retrySleep: 테스트용 sleep 주입 (nil이면 기본 ctxutil.SleepWithContext 사용)
	retrySleep func(ctx context.Context, d time.Duration) bool
}

// AcquireIngestionLease - ingestion 리스를 획득한다. 이미 다른 프로세스가 소유 중이면 에러 반환.
func AcquireIngestionLease(
	ctx context.Context,
	cacheSvc cache.Client,
	role string,
	logger *slog.Logger,
) (*IngestionLease, error) {
	if cacheSvc == nil {
		return nil, fmt.Errorf("acquire ingestion lease: cache service must not be nil")
	}
	if role == "" {
		return nil, fmt.Errorf("acquire ingestion lease: role must not be empty")
	}
	if logger == nil {
		logger = slog.Default()
	}

	owner := fmt.Sprintf("%s:%d:%d", role, os.Getpid(), time.Now().UnixNano())
	acquired, err := cacheSvc.SetNX(ctx, IngestionLeaseKey, owner, ingestionLeaseTTL)
	if err != nil {
		return nil, fmt.Errorf("acquire ingestion lease: setnx failed: %w", err)
	}
	if !acquired {
		return nil, fmt.Errorf("acquire ingestion lease: lock already held: key=%s", IngestionLeaseKey)
	}

	logger.Info("Ingestion lease acquired",
		slog.String("event", "ingestion_lease_acquired"),
		slog.String("role", role),
		slog.String("key", IngestionLeaseKey),
		slog.String("owner", owner),
	)

	return &IngestionLease{
		cacheSvc:      cacheSvc,
		key:           IngestionLeaseKey,
		owner:         owner,
		role:          role,
		ttl:           ingestionLeaseTTL,
		renewInterval: ingestionLeaseRenewGap,
		logger:        logger,
	}, nil
}

// StartRenewLoop - 갱신 루프를 시작한다. ctx 취소 시 종료, 소유권 상실/갱신 실패 시 errCh로 전달.
func (l *IngestionLease) StartRenewLoop(ctx context.Context, errCh chan<- error) {
	if l == nil {
		return
	}

	renewInterval := l.renewInterval
	if renewInterval <= 0 {
		renewInterval = ingestionLeaseRenewGap
	}

	ticker := time.NewTicker(renewInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := l.renew(ctx); err != nil {
				if errors.Is(err, errIngestionLeaseOwnershipLost) {
					l.logger.Error("Ingestion lease ownership lost",
						slog.String("event", "ingestion_lease_lost"),
						slog.String("role", l.role),
						slog.String("key", l.key),
						slog.String("owner", l.owner),
						slog.Any("error", err),
					)
					if errCh != nil {
						select {
						case errCh <- fmt.Errorf("ingestion lease ownership lost: %w", err):
						default:
						}
					}
					return
				}
				l.logger.Error("Ingestion lease renew exhausted all retries",
					slog.String("event", "ingestion_lease_renew_failed"),
					slog.String("role", l.role),
					slog.String("key", l.key),
					slog.Any("error", err),
				)
				if errCh != nil {
					select {
					case errCh <- fmt.Errorf("ingestion lease renew failed: %w", err):
					default:
					}
				}
				return
			}
		}
	}
}

func (l *IngestionLease) renew(ctx context.Context) error {
	err := retry.WithRetry(ctx, retry.RetryOptions{
		MaxAttempts: 3,
		BaseDelay:   1 * time.Second,
		Jitter:      500 * time.Millisecond,
		Sleep:       l.retrySleep,
		ShouldRetry: func(err error) bool {
			// 소유권 상실은 재시도 무의미
			return !errors.Is(err, errIngestionLeaseOwnershipLost)
		},
		OnRetry: func(attempt int, err error, delay time.Duration) {
			l.logger.Warn("Ingestion lease renew retrying",
				slog.String("event", "ingestion_lease_renew_retry"),
				slog.String("key", l.key),
				slog.Int("attempt", attempt),
				slog.Duration("delay", delay),
				slog.Any("error", err),
			)
		},
	}, func(ctx context.Context) error {
		renewed, err := l.cacheSvc.CompareAndExpire(ctx, l.key, l.owner, l.ttl)
		if err != nil {
			return fmt.Errorf("renew ingestion lease: %w", err)
		}
		if !renewed {
			return fmt.Errorf("renew ingestion lease: %w: key=%s", errIngestionLeaseOwnershipLost, l.key)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("renew ingestion lease with retry: %w", err)
	}

	return nil
}

// Release - ingestion 리스를 해제한다.
func (l *IngestionLease) Release(ctx context.Context) error {
	if l == nil {
		return nil
	}

	released, err := l.cacheSvc.CompareAndDelete(ctx, l.key, l.owner)
	if err != nil {
		return fmt.Errorf("release ingestion lease: compare-and-delete failed: %w", err)
	}
	if !released {
		return fmt.Errorf("release ingestion lease: lease ownership mismatch")
	}

	l.logger.Info("Ingestion lease released",
		slog.String("event", "ingestion_lease_released"),
		slog.String("role", l.role),
		slog.String("key", l.key),
		slog.String("owner", l.owner),
	)
	return nil
}
