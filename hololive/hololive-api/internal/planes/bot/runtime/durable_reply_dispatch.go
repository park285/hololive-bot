package botruntime

import (
	"context"
	"crypto/rand"
	"errors"
	"log/slog"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/bot/orchestration/transport"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/durability"
)

func (r *durableRuntime) runOutboxWorker(ctx context.Context) {
	defer r.wg.Done()
	idleDelay := durablePollEvery
	for ctx.Err() == nil {
		if r.processNextOutbox(ctx) {
			idleDelay = durablePollEvery
			continue
		}
		durableClaimIdleTotal.WithLabelValues("outbox").Inc()
		if !waitDurableWake(ctx, idleDelay, r.outboxWake) {
			return
		}
		idleDelay = nextDurableIdleDelay(idleDelay)
	}
}

func (r *durableRuntime) processNextOutbox(ctx context.Context) bool {
	token := rand.Text()
	claim, err := r.outbox.Claim(ctx, token, durableClaimLease)
	if err != nil {
		r.logError("claim reply outbox", err)
		return false
	}
	if claim == nil {
		return false
	}
	r.dispatchOutboxClaim(ctx, claim, token)
	return true
}

func (r *durableRuntime) dispatchOutboxClaim(ctx context.Context, claim *durability.ReplyOutboxClaim, token string) {
	accepted := false
	dispatchCtx, cancel := context.WithTimeout(ctx, r.dispatchBudget)
	err := transport.DispatchStoredReply(dispatchCtx, r.irisClient, claim.RoomID, claim.Payload, claim.ClientRequestID,
		r.acceptanceHook(claim.ID, token, &accepted))
	cancel()
	applied, settleErr := r.settleOutboxDispatch(ctx, claim, token, accepted, err)
	r.finishOutboxSettlement(claim, accepted, err, applied, settleErr)
}

func (r *durableRuntime) acceptanceHook(id int64, token string, accepted *bool) transport.ReplyAcceptedHook {
	return func(ctx context.Context, requestID string) error {
		*accepted = true
		applied, err := r.outbox.MarkAccepted(ctx, id, token, requestID)
		if err != nil {
			return err
		}
		if !applied {
			return errors.New("reply outbox acceptance lost its claim")
		}
		return nil
	}
}

func (r *durableRuntime) finishOutboxSettlement(claim *durability.ReplyOutboxClaim, accepted bool, dispatchErr error, applied bool, settleErr error) {
	switch {
	case settleErr != nil:
		r.logError("settle reply outbox", settleErr)
	case !applied && !accepted:
		r.logError("settle reply outbox", errors.New("reply outbox settlement lost its claim"))
	case applied:
		r.observeOutboxSettlement(claim, replyOutboxSettlementStatus(accepted, claim.Attempts, dispatchErr))
	}
}

func (r *durableRuntime) observeOutboxSettlement(claim *durability.ReplyOutboxClaim, status string) {
	if r.logger == nil || status == durability.ReplyOutboxHandoffCompleted {
		return
	}
	attrs := []any{slog.String("status", status), slog.Int("attempts", int(claim.Attempts))}
	if status == durability.ReplyOutboxRetryablePreDispatch || status == durability.ReplyOutboxOutcomeUnknown {
		r.logger.Warn("reply outbox dispatch deferred", attrs...)
		return
	}
	r.logger.Error("reply outbox dispatch reached non-success terminal state", attrs...)
}

func (r *durableRuntime) settleOutboxDispatch(ctx context.Context, claim *durability.ReplyOutboxClaim, token string, accepted bool, dispatchErr error) (bool, error) {
	status := replyOutboxSettlementStatus(accepted, claim.Attempts, dispatchErr)
	retryAfter := replyOutboxRetryAfter(status, claim.Attempts)
	return r.outbox.Settle(ctx, durability.ReplyOutboxSettlement{
		ID: claim.ID, ClaimToken: token, Status: status, LastError: errorText(dispatchErr), RetryAfter: retryAfter,
	})
}
