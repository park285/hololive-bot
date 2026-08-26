package botruntime

import (
	"context"
	"errors"
	"fmt"
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
	panicguard.Go(r.logger, "durable-inbox-queue-sampler", func() { r.inboxSampler.Run(runCtx) })
	panicguard.Go(r.logger, "durable-outbox-queue-sampler", func() { r.outboxSampler.Run(runCtx) })

	if r.inboxEnabled {
		r.inboxTracker.StartWorkers(r.inboxWorkers)

		for range r.inboxWorkers {
			r.wg.Go(func() {
				panicguard.Run(r.logger, "durable-inbox-worker", func() { r.runInboxWorker(runCtx) })
			})
		}
	}

	if r.outboxEnabled {
		r.outboxTracker.StartWorkers(r.outboxWorkers)

		for range r.outboxWorkers {
			r.wg.Go(func() {
				panicguard.Run(r.logger, "durable-outbox-worker", func() { r.runOutboxWorker(runCtx) })
			})
		}
	}

	r.wg.Go(func() {
		panicguard.Run(r.logger, "durable-maintenance", func() { r.runMaintenance(runCtx) })
	})
}

func (r *durableRuntime) Stop(ctx context.Context) error {
	if r == nil || r.cancel == nil {
		return nil
	}

	r.cancel()

	done := make(chan struct{})

	panicguard.Go(r.logger, "durable-stop-wait", func() { r.wg.Wait(); close(done) })

	if err := waitForDurableStop(ctx, done); err != nil {
		return fmt.Errorf("%w", err)
	}

	r.stopWorkerTrackers()

	return nil
}

func waitForDurableStop(ctx context.Context, done <-chan struct{}) error {
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("wait for durable workers: %w", ctx.Err())
	}
}

func (r *durableRuntime) stopWorkerTrackers() {
	if r.inboxEnabled {
		r.inboxTracker.StopWorkers(r.inboxWorkers)
	}

	if r.outboxEnabled {
		r.outboxTracker.StopWorkers(r.outboxWorkers)
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
	return r.heartbeatEvery
}

func (r *durableRuntime) claimLeaseDuration() time.Duration {
	return r.claimLease
}

func (r *durableRuntime) ownershipSafetyMarginDuration() time.Duration {
	return r.ownershipSafetyMargin
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
	if inboxErr != nil {
		inboxErr = fmt.Errorf("inbox heartbeat: %w", inboxErr)
	}

	commandApplied, commandErr = r.commandHeartbeat(attemptCtx, messageID, token)
	if commandErr != nil {
		commandErr = fmt.Errorf("command heartbeat: %w", commandErr)
	}

	return leaseUntil, inboxApplied, commandApplied, inboxErr, commandErr
}
