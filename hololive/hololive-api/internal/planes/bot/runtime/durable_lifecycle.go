package botruntime

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/privacylog"
	"github.com/kapu/hololive-shared/pkg/panicguard"
)

const (
	ownershipCancellationLost        = "ownership_lost"
	ownershipCancellationUnconfirmed = "ownership_unconfirmed"
)

type heartbeatRenewalOutcome uint8

const (
	heartbeatRenewed heartbeatRenewalOutcome = iota
	heartbeatRetry
	heartbeatStopped
)

func (r *durableRuntime) Start(ctx context.Context) {
	if r == nil {
		return
	}
	runCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	r.cancel = cancel
	for range r.workers {
		r.wg.Add(2)
		panicguard.Go(r.logger, "durable-inbox-worker", func() { r.runInboxWorker(runCtx) })
		panicguard.Go(r.logger, "durable-outbox-worker", func() { r.runOutboxWorker(runCtx) })
	}
	r.wg.Add(1)
	panicguard.Go(r.logger, "durable-maintenance", func() { r.runMaintenance(runCtx) })
}

func (r *durableRuntime) Stop(ctx context.Context) error {
	if r == nil || r.cancel == nil {
		return nil
	}
	r.cancel()
	done := make(chan struct{})
	panicguard.Go(r.logger, "durable-stop-wait", func() { r.wg.Wait(); close(done) })
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (r *durableRuntime) runClaimHeartbeat(ctx context.Context, messageID, token string, leaseUntil time.Time, cancelCommand context.CancelFunc) {
	for {
		renewedUntil, keepRunning := r.runHeartbeatRenewalCycle(ctx, messageID, token, leaseUntil, cancelCommand)
		if !keepRunning {
			return
		}
		leaseUntil = renewedUntil
	}
}

func (r *durableRuntime) runHeartbeatRenewalCycle(
	ctx context.Context,
	messageID string,
	token string,
	leaseUntil time.Time,
	cancelCommand context.CancelFunc,
) (time.Time, bool) {
	ownershipDeadline, ready := r.waitForHeartbeatRenewal(ctx, messageID, leaseUntil, cancelCommand)
	if !ready {
		return time.Time{}, false
	}
	renewedUntil, outcome := r.renewHeartbeatClaim(ctx, messageID, token, ownershipDeadline, cancelCommand)
	if outcome == heartbeatStopped {
		return time.Time{}, false
	}
	if outcome == heartbeatRenewed {
		return renewedUntil, true
	}
	return leaseUntil, true
}

func (r *durableRuntime) waitForHeartbeatRenewal(
	ctx context.Context,
	messageID string,
	leaseUntil time.Time,
	cancelCommand context.CancelFunc,
) (time.Time, bool) {
	ownershipDeadline := r.ownershipDeadline(leaseUntil)
	remaining := ownershipDeadline.Sub(r.nowTime())
	if remaining <= 0 {
		r.cancelCommandForOwnership(messageID, ownershipCancellationUnconfirmed, cancelCommand)
		return time.Time{}, false
	}
	return ownershipDeadline, r.waitHeartbeat(ctx, min(r.heartbeatInterval(), remaining))
}

func (r *durableRuntime) renewHeartbeatClaim(
	ctx context.Context,
	messageID string,
	token string,
	ownershipDeadline time.Time,
	cancelCommand context.CancelFunc,
) (time.Time, heartbeatRenewalOutcome) {
	heartbeatCtx, cancelHeartbeat := context.WithDeadline(ctx, ownershipDeadline)
	defer cancelHeartbeat()
	renewedUntil, owned, confirmed := r.heartbeatClaim(heartbeatCtx, messageID, token)
	if confirmed && owned {
		return renewedUntil, heartbeatRenewed
	}
	if confirmed {
		r.cancelCommandForOwnership(messageID, ownershipCancellationLost, cancelCommand)
		return time.Time{}, heartbeatStopped
	}
	if r.nowTime().Before(ownershipDeadline) {
		return time.Time{}, heartbeatRetry
	}
	r.cancelCommandForOwnership(messageID, ownershipCancellationUnconfirmed, cancelCommand)
	return time.Time{}, heartbeatStopped
}

func (r *durableRuntime) nowTime() time.Time {
	if r.now != nil {
		return r.now()
	}
	return time.Now()
}

func (r *durableRuntime) waitHeartbeat(ctx context.Context, delay time.Duration) bool {
	if r.heartbeatWait != nil {
		return r.heartbeatWait(ctx, delay)
	}
	return waitDurable(ctx, delay)
}

func (r *durableRuntime) ownershipDeadline(leaseUntil time.Time) time.Time {
	return leaseUntil.Add(-r.ownershipSafetyMarginDuration())
}

func (r *durableRuntime) cancelCommandForOwnership(messageID, reason string, cancelCommand context.CancelFunc) {
	durableOwnershipCancellationTotal.WithLabelValues(reason).Inc()
	if r.logger != nil {
		r.logger.Error("durable command canceled before ownership lease expiry",
			slog.String("reason", reason),
			slog.String("message_token", privacylog.Pseudonym(messageID)))
	}
	cancelCommand()
}

func (r *durableRuntime) heartbeatInterval() time.Duration {
	if r.heartbeatEvery > 0 {
		return r.heartbeatEvery
	}
	return durableHeartbeatEvery
}

func (r *durableRuntime) claimLeaseDuration() time.Duration {
	if r.claimLease > 0 {
		return r.claimLease
	}
	return durableClaimLease
}

func (r *durableRuntime) ownershipSafetyMarginDuration() time.Duration {
	lease := r.claimLeaseDuration()
	margin := r.ownershipSafetyMargin
	if margin <= 0 {
		margin = durableOwnershipSafetyMargin
	}
	if margin >= lease {
		return lease / 2
	}
	return margin
}

func (r *durableRuntime) heartbeatClaim(ctx context.Context, messageID, token string) (leaseUntil time.Time, owned, confirmed bool) {
	for attempt := range 3 {
		renewedUntil, inboxApplied, commandApplied, inboxErr, commandErr := r.heartbeatAttempt(ctx, messageID, token)
		if inboxErr == nil && commandErr == nil {
			return renewedUntil, inboxApplied && commandApplied, true
		}
		if attempt < 2 && r.waitHeartbeatRetry(ctx) {
			continue
		}
		r.logError("heartbeat durable command claim", errors.Join(inboxErr, commandErr))
		return time.Time{}, true, false
	}
	return time.Time{}, true, false
}

func (r *durableRuntime) waitHeartbeatRetry(ctx context.Context) bool {
	delay := r.heartbeatRetryDelay
	deadline, bounded := ctx.Deadline()
	if bounded {
		remaining := deadline.Sub(r.nowTime())
		if remaining <= 0 {
			return false
		}
		delay = min(delay, remaining)
	}
	if !r.waitHeartbeat(ctx, delay) {
		return false
	}
	return !bounded || r.nowTime().Before(deadline)
}

func (r *durableRuntime) heartbeatAttempt(ctx context.Context, messageID, token string) (leaseUntil time.Time, inboxApplied, commandApplied bool, inboxErr, commandErr error) {
	budget := r.heartbeatAttemptBudget
	if budget <= 0 {
		budget = 5 * time.Second
	}
	attemptCtx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	leaseUntil, inboxApplied, inboxErr = r.inboxHeartbeat(attemptCtx, messageID, token, r.claimLeaseDuration())
	commandApplied, commandErr = r.commandHeartbeat(attemptCtx, messageID, token)
	return leaseUntil, inboxApplied, commandApplied, inboxErr, commandErr
}
