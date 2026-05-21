package producerruntime

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/kapu/hololive-shared/pkg/service/youtube/poller"
	"github.com/kapu/hololive-youtube-producer/internal/runtime/readiness"
)

func startRecoveryLoop(
	ctx context.Context,
	claimer poller.JobClaimer,
	state *readiness.State,
	baseInterval time.Duration,
	maxInterval time.Duration,
	logger *slog.Logger,
	onResume func(),
) (stop func()) {
	if ctx == nil {
		ctx = context.Background()
	}
	if baseInterval <= 0 {
		baseInterval = 5 * time.Second
	}
	if maxInterval <= 0 {
		maxInterval = 60 * time.Second
	}
	if maxInterval < baseInterval {
		maxInterval = baseInterval
	}
	loopCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	var stopOnce sync.Once
	if claimer == nil || state == nil {
		close(done)
		return func() {
			stopOnce.Do(cancel)
			<-done
		}
	}

	go func() {
		defer close(done)
		runRecoveryLoop(loopCtx, claimer, state, baseInterval, maxInterval, logger, onResume)
	}()

	return func() {
		stopOnce.Do(cancel)
		<-done
	}
}

func runRecoveryLoop(
	ctx context.Context,
	claimer poller.JobClaimer,
	state *readiness.State,
	baseInterval time.Duration,
	maxInterval time.Duration,
	logger *slog.Logger,
	onResume func(),
) {
	backoff := baseInterval
	wasUnavailable := !state.LeaseAvailable()
	for {
		if ctx.Err() != nil {
			return
		}
		wait, nextBackoff, nextWasUnavailable := recoveryLoopIteration(
			ctx,
			claimer,
			state,
			baseInterval,
			maxInterval,
			backoff,
			logger,
			onResume,
			wasUnavailable,
		)
		backoff = nextBackoff
		wasUnavailable = nextWasUnavailable
		if !sleepRecoveryLoop(ctx, wait) {
			return
		}
	}
}

func recoveryLoopIteration(
	ctx context.Context,
	claimer poller.JobClaimer,
	state *readiness.State,
	baseInterval time.Duration,
	maxInterval time.Duration,
	backoff time.Duration,
	logger *slog.Logger,
	onResume func(),
	wasUnavailable bool,
) (time.Duration, time.Duration, bool) {
	if state.LeaseAvailable() {
		return baseInterval, baseInterval, false
	}
	wasUnavailable = true
	status, claim, err := claimer.TryClaim(ctx, readinessProbePollerName, readinessProbeChannelID, readinessProbeTTL, readinessProbeTTL)
	if err != nil || status.Result == poller.JobClaimUnavailable {
		logRecoveryLoopDebug(logger, "active_active_recovery_probe_failed", err, status.Result)
		return backoff, nextRecoveryBackoff(backoff, maxInterval), true
	}
	available := handleRecoveryLoopClaim(ctx, status, claim, state, logger)
	notifyRecoveryResume(wasUnavailable, available, onResume)
	return baseInterval, baseInterval, !available
}

func handleRecoveryLoopClaim(
	ctx context.Context,
	status poller.JobClaimStatus,
	claim poller.JobClaim,
	state *readiness.State,
	logger *slog.Logger,
) bool {
	switch status.Result {
	case poller.JobClaimAcquired:
		if claim != nil {
			releaseReadinessProbeClaim(ctx, claim, logger)
		}
		state.MarkLeaseAvailable()
		logRecoveryLoopInfo(logger, "active_active_resumed", readinessProbePollerName)
		return true
	case poller.JobClaimPeerOwned, poller.JobClaimAlreadyCompleted:
		state.MarkLeaseAvailable()
		logRecoveryLoopDebug(logger, "active_active_recovery_probe_available", nil, status.Result)
		return true
	default:
		logRecoveryLoopDebug(logger, "active_active_recovery_probe_unexpected_result", nil, status.Result)
		return false
	}
}

func notifyRecoveryResume(wasUnavailable bool, available bool, onResume func()) {
	if wasUnavailable && available && onResume != nil {
		onResume()
	}
}

func nextRecoveryBackoff(current time.Duration, maxInterval time.Duration) time.Duration {
	if current <= 0 {
		return maxInterval
	}
	next := current * 2
	if current >= 10*time.Second && next < 30*time.Second {
		next = 30 * time.Second
	}
	if next > maxInterval {
		return maxInterval
	}
	return next
}

func sleepRecoveryLoop(ctx context.Context, duration time.Duration) bool {
	if duration <= 0 {
		duration = 5 * time.Second
	}
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func logRecoveryLoopInfo(logger *slog.Logger, message string, pollerName string) {
	if logger != nil {
		logger.Info(message, slog.String("poller", pollerName))
	}
}

func logRecoveryLoopDebug(logger *slog.Logger, message string, err error, result poller.JobClaimResult) {
	if logger == nil {
		return
	}
	attrs := []any{slog.String("poller", readinessProbePollerName), slog.String("result", string(result))}
	if err != nil {
		attrs = append(attrs, slog.Any("error", err))
	}
	logger.Debug(message, attrs...)
}
