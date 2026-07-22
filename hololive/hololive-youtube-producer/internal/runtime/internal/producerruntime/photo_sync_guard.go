package producerruntime

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/kapu/hololive-shared/pkg/panicguard"
	"github.com/kapu/hololive-youtube-producer/internal/runtime/ingestionlease"
)

type photoSyncService interface {
	Start(context.Context)
}

type leasedPhotoSyncService struct {
	inner           photoSyncService
	guard           *ingestionlease.JobRunGuard
	logger          *slog.Logger
	leaseTTL        time.Duration
	retryInterval   time.Duration
	shutdownTimeout time.Duration
	cleanupTimeout  time.Duration
}

const (
	photoSyncLeasePollerName       = "photo-sync"
	photoSyncLeaseChannelID        = "__global__"
	defaultPhotoSyncLeaseTTL       = 2 * time.Minute
	defaultPhotoSyncRetryWait      = 30 * time.Second
	defaultPhotoSyncShutdownWait   = 30 * time.Second
	defaultPhotoSyncCleanupTimeout = 5 * time.Second
)

func newLeasedPhotoSyncService(inner photoSyncService, guard *ingestionlease.JobRunGuard, logger *slog.Logger) photoSyncService {
	if inner == nil || guard == nil {
		return inner
	}
	return &leasedPhotoSyncService{
		inner:           inner,
		guard:           guard,
		logger:          logger,
		leaseTTL:        defaultPhotoSyncLeaseTTL,
		retryInterval:   defaultPhotoSyncRetryWait,
		shutdownTimeout: defaultPhotoSyncShutdownWait,
		cleanupTimeout:  defaultPhotoSyncCleanupTimeout,
	}
}

func (s *leasedPhotoSyncService) Start(ctx context.Context) {
	if !s.isConfigured() {
		return
	}
	if ctx == nil {
		return
	}
	for ctx.Err() == nil {
		claim, ok := s.tryAcquire(ctx)
		if !ok {
			continue
		}
		if !s.runOwned(ctx, claim) {
			return
		}
	}
}

func (s *leasedPhotoSyncService) isConfigured() bool {
	return s != nil && s.inner != nil && s.guard != nil
}

func (s *leasedPhotoSyncService) tryAcquire(ctx context.Context) (*ingestionlease.JobRunClaim, bool) {
	leaseTTL := s.effectiveLeaseTTL()
	retryInterval := s.effectiveRetryInterval()
	status, claim, err := s.guard.TryLease(ctx, ingestionlease.JobIdentity{
		PollerName: photoSyncLeasePollerName,
		ChannelID:  photoSyncLeaseChannelID,
	}, leaseTTL, retryInterval)
	if err != nil {
		s.logWarn(ctx, "photo_sync_lease_acquire_failed", err)
		s.waitBeforeRetry(ctx, retryInterval)
		return nil, false
	}
	if status.Result != ingestionlease.JobClaimAcquired {
		s.waitBeforeRetry(ctx, retryWait(status.RetryAfter, retryInterval))
		return nil, false
	}
	if claim == nil {
		s.logWarn(ctx, "photo_sync_lease_acquired_without_claim", errors.New("photo sync lease claim is nil"))
		s.waitBeforeRetry(ctx, retryInterval)
		return nil, false
	}
	s.logInfo("photo_sync_lease_acquired")
	return claim, true
}

// runOwned는 내부 작업이 실제로 종료된 뒤에만 lease를 명시적으로 반납한다.
// 종료 제한을 넘긴 작업은 lease를 반납하지 않고 TTL 만료에 맡겨 다음 owner와의 중첩을 줄인다.
func (s *leasedPhotoSyncService) runOwned(ctx context.Context, claim *ingestionlease.JobRunClaim) bool {
	if claim == nil {
		s.logWarn(ctx, "photo_sync_lease_claim_missing", errors.New("photo sync lease claim is nil"))
		return false
	}

	childCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	panicguard.Go(s.logger, "photo-sync-leased-service", func() {
		defer close(done)
		s.inner.Start(childCtx)
	})

	s.renewUntilStopped(ctx, claim, cancel, done)
	cancel()
	if !s.waitForInnerStop(done) {
		s.logShutdownTimeout(ctx)
		return false
	}

	s.releaseClaim(ctx, claim)
	return true
}

func (s *leasedPhotoSyncService) renewUntilStopped(
	ctx context.Context,
	claim *ingestionlease.JobRunClaim,
	cancel context.CancelFunc,
	done <-chan struct{},
) {
	leaseTTL := s.effectiveLeaseTTL()
	renewInterval := leaseTTL / 3
	if renewInterval <= 0 {
		renewInterval = leaseTTL
	}
	ticker := time.NewTicker(renewInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-done:
			return
		case <-ticker.C:
			renewed, err := claim.Renew(ctx, leaseTTL)
			if err != nil || !renewed {
				s.logWarn(ctx, "photo_sync_lease_lost", err)
				cancel()
				return
			}
		}
	}
}

func (s *leasedPhotoSyncService) waitForInnerStop(done <-chan struct{}) bool {
	select {
	case <-done:
		return true
	default:
	}

	timer := time.NewTimer(s.effectiveShutdownTimeout())
	defer timer.Stop()
	select {
	case <-done:
		return true
	case <-timer.C:
		return false
	}
}

func (s *leasedPhotoSyncService) releaseClaim(ctx context.Context, claim *ingestionlease.JobRunClaim) {
	if ctx == nil {
		if s.logger != nil {
			s.logger.Warn("photo_sync_lease_release_skipped",
				slog.String("task", photoSyncLeasePollerName),
				slog.String("reason", "release context is nil"),
			)
		}
		return
	}
	releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.effectiveCleanupTimeout())
	defer cancel()

	released, err := claim.Release(releaseCtx)
	if err != nil {
		s.logWarn(ctx, "photo_sync_lease_release_failed", err)
		return
	}
	if !released {
		s.logWarn(ctx, "photo_sync_lease_release_skipped", nil)
	}
}

func (s *leasedPhotoSyncService) waitBeforeRetry(ctx context.Context, wait time.Duration) {
	if wait <= 0 {
		wait = s.effectiveRetryInterval()
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return
	case <-timer.C:
		return
	}
}

func retryWait(retryAfter, fallback time.Duration) time.Duration {
	if retryAfter > 0 && retryAfter < fallback {
		return retryAfter
	}
	return fallback
}

func (s *leasedPhotoSyncService) effectiveLeaseTTL() time.Duration {
	if s.leaseTTL > 0 {
		return s.leaseTTL
	}
	return defaultPhotoSyncLeaseTTL
}

func (s *leasedPhotoSyncService) effectiveRetryInterval() time.Duration {
	if s.retryInterval > 0 {
		return s.retryInterval
	}
	return defaultPhotoSyncRetryWait
}

func (s *leasedPhotoSyncService) effectiveShutdownTimeout() time.Duration {
	if s.shutdownTimeout > 0 {
		return s.shutdownTimeout
	}
	return defaultPhotoSyncShutdownWait
}

func (s *leasedPhotoSyncService) effectiveCleanupTimeout() time.Duration {
	if s.cleanupTimeout > 0 {
		return s.cleanupTimeout
	}
	return defaultPhotoSyncCleanupTimeout
}

func (s *leasedPhotoSyncService) logInfo(message string) {
	if s.logger != nil {
		s.logger.Info(message, slog.String("task", photoSyncLeasePollerName))
	}
}

func (s *leasedPhotoSyncService) logWarn(ctx context.Context, message string, err error) {
	if s.logger == nil {
		return
	}
	attrs := []slog.Attr{slog.String("task", photoSyncLeasePollerName)}
	if err != nil {
		attrs = append(attrs, slog.Any("error", err))
	}
	if ctx == nil {
		if err != nil {
			s.logger.Warn(message, slog.String("task", photoSyncLeasePollerName), slog.Any("error", err))
		} else {
			s.logger.Warn(message, slog.String("task", photoSyncLeasePollerName))
		}
		return
	}
	s.logger.LogAttrs(ctx, slog.LevelWarn, message, attrs...)
}

func (s *leasedPhotoSyncService) logShutdownTimeout(ctx context.Context) {
	if s.logger == nil {
		return
	}
	if ctx == nil {
		s.logger.Error("photo_sync_inner_shutdown_timed_out",
			slog.String("task", photoSyncLeasePollerName),
			slog.Duration("shutdown_timeout", s.effectiveShutdownTimeout()),
			slog.String("lease_cleanup", "deferred_to_ttl"),
		)
		return
	}
	s.logger.LogAttrs(ctx, slog.LevelError, "photo_sync_inner_shutdown_timed_out",
		slog.String("task", photoSyncLeasePollerName),
		slog.Duration("shutdown_timeout", s.effectiveShutdownTimeout()),
		slog.String("lease_cleanup", "deferred_to_ttl"),
	)
}
