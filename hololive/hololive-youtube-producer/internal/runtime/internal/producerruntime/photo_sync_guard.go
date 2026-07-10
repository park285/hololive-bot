package producerruntime

import (
	"context"
	"log/slog"
	"time"

	"github.com/kapu/hololive-shared/pkg/panicguard"
	"github.com/kapu/hololive-youtube-producer/internal/runtime/ingestionlease"
)

type photoSyncService interface {
	Start(context.Context)
}

type leasedPhotoSyncService struct {
	inner         photoSyncService
	guard         *ingestionlease.JobRunGuard
	logger        *slog.Logger
	leaseTTL      time.Duration
	retryInterval time.Duration
}

const (
	photoSyncLeasePollerName  = "photo-sync"
	photoSyncLeaseChannelID   = "__global__"
	defaultPhotoSyncLeaseTTL  = 2 * time.Minute
	defaultPhotoSyncRetryWait = 30 * time.Second
)

func newLeasedPhotoSyncService(inner photoSyncService, guard *ingestionlease.JobRunGuard, logger *slog.Logger) photoSyncService {
	if inner == nil || guard == nil {
		return inner
	}
	return &leasedPhotoSyncService{
		inner:         inner,
		guard:         guard,
		logger:        logger,
		leaseTTL:      defaultPhotoSyncLeaseTTL,
		retryInterval: defaultPhotoSyncRetryWait,
	}
}

func (s *leasedPhotoSyncService) Start(ctx context.Context) {
	for ctx.Err() == nil {
		claim, ok := s.tryAcquire(ctx)
		if !ok {
			continue
		}
		s.runOwned(ctx, claim)
	}
}

func (s *leasedPhotoSyncService) tryAcquire(ctx context.Context) (*ingestionlease.JobRunClaim, bool) {
	status, claim, err := s.guard.TryLease(ctx, ingestionlease.JobIdentity{
		PollerName: photoSyncLeasePollerName,
		ChannelID:  photoSyncLeaseChannelID,
	}, s.leaseTTL, s.retryInterval)
	if err != nil {
		s.logWarn(ctx, "photo_sync_lease_acquire_failed", err)
		s.waitBeforeRetry(ctx, s.retryInterval)
		return nil, false
	}
	if status.Result != ingestionlease.JobClaimAcquired {
		s.waitBeforeRetry(ctx, retryWait(status.RetryAfter, s.retryInterval))
		return nil, false
	}
	s.logInfo("photo_sync_lease_acquired")
	return claim, true
}

func (s *leasedPhotoSyncService) runOwned(ctx context.Context, claim *ingestionlease.JobRunClaim) {
	childCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	panicguard.Go(s.logger, "photo-sync-leased-service", func() {
		defer close(done)
		s.inner.Start(childCtx)
	})
	s.renewUntilStopped(ctx, claim, cancel, done)
	cancel()
	if _, err := claim.Release(context.WithoutCancel(ctx)); err != nil {
		s.logWarn(ctx, "photo_sync_lease_release_failed", err)
	}
}

func (s *leasedPhotoSyncService) renewUntilStopped(
	ctx context.Context,
	claim *ingestionlease.JobRunClaim,
	cancel context.CancelFunc,
	done <-chan struct{},
) {
	ticker := time.NewTicker(s.leaseTTL / 3)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-done:
			return
		case <-ticker.C:
			renewed, err := claim.Renew(ctx, s.leaseTTL)
			if err != nil || !renewed {
				s.logWarn(ctx, "photo_sync_lease_lost", err)
				cancel()
				<-done
				return
			}
		}
	}
}

func (s *leasedPhotoSyncService) waitBeforeRetry(ctx context.Context, wait time.Duration) {
	if wait <= 0 {
		wait = s.retryInterval
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
	s.logger.LogAttrs(ctx, slog.LevelWarn, message, attrs...)
}
