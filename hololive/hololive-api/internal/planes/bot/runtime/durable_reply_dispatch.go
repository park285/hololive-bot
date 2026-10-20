package botruntime

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/park285/shared-go/v2/pkg/db/pgxdb"
	"github.com/park285/shared-go/v2/pkg/workercontract"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/bot/orchestration/transport"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/durability"
)

func (r *durableRuntime) runOutboxWorker(ctx context.Context) {
	runDurableWorker(ctx, "outbox", r.processNextOutbox, r.outboxWake, r.outboxPollSlot, r.outboxPollEvery)
}

func (r *durableRuntime) processNextOutbox(ctx context.Context, claimed func()) bool {
	token := rand.Text()

	claim, err := r.outbox.Claim(ctx, token, r.outboxClaimLease)
	if err != nil {
		r.logError("claim reply outbox", err)

		return false
	}

	if claim == nil {
		return false
	}

	claimed()

	attemptID := r.outboxTracker.BeginAttempt(time.Now())
	defer r.outboxTracker.EndAttempt(attemptID)

	r.dispatchOutboxClaim(ctx, claim, token)

	return true
}

// dispatchOutboxClaim은 admission까지만 발송 예산(dispatchBudget) 안에서 기다린다. Handoff 확인은 accepted
// 정산 뒤 reconciler가 lease 없이 이어 받는다.
func (r *durableRuntime) dispatchOutboxClaim(ctx context.Context, claim *durability.ReplyOutboxClaim, token string) {
	dispatchCtx, cancel := context.WithTimeout(ctx, r.dispatchBudget)
	requestID, err := transport.DispatchStoredReply(dispatchCtx, r.irisClient, claim.RoomID, claim.Payload, claim.ClientRequestID)

	cancel()

	status := replyOutboxSettlementStatusWithMaxAttempts(claim.Attempts, r.outboxMaxAttempts, err)
	applied, settleErr := r.settleOutboxDispatch(ctx, claim, token, status, requestID, err)
	r.finishOutboxSettlement(claim, status, err, applied, settleErr)
}

func (r *durableRuntime) finishOutboxSettlement(claim *durability.ReplyOutboxClaim, status string, dispatchErr error, applied bool, settleErr error) {
	outcome := workercontract.AttemptFailed

	switch {
	case settleErr != nil:
		r.logError("settle reply outbox", settleErr)

		outcome = workercontract.AttemptOutcomeUnknown
	case !applied:
		r.logError("settle reply outbox", errors.New("reply outbox settlement lost its claim"))

		outcome = workercontract.AttemptOutcomeUnknown
	case applied:
		r.observeOutboxSettlement(claim, status)

		outcome = outboxAttemptOutcome(status, dispatchErr)
	}

	r.outboxTotals.RecordAttempt(outcome)
}

func outboxAttemptOutcome(status string, err error) workercontract.AttemptOutcome {
	if status == durability.ReplyOutboxAccepted {
		return workercontract.AttemptSuccess
	}

	if status == durability.ReplyOutboxOutcomeUnknown || status == durability.ReplyOutboxManualReview {
		return workercontract.AttemptOutcomeUnknown
	}

	return failureAttemptOutcome(err)
}

func (r *durableRuntime) observeOutboxSettlement(claim *durability.ReplyOutboxClaim, status string) {
	if r.logger == nil || status == durability.ReplyOutboxAccepted {
		return
	}

	attrs := []any{slog.String("status", status), slog.Int("attempts", int(claim.Attempts))}
	if status == durability.ReplyOutboxRetryablePreDispatch || status == durability.ReplyOutboxOutcomeUnknown {
		r.logger.Warn("reply outbox dispatch deferred", attrs...)

		return
	}

	r.logger.Error("reply outbox dispatch reached non-success terminal state", attrs...)
}

func (r *durableRuntime) settleOutboxDispatch(
	ctx context.Context,
	claim *durability.ReplyOutboxClaim,
	token, status, irisRequestID string,
	dispatchErr error,
) (bool, error) {
	// shutdown 취소 뒤에도 이미 관측한 전달 결과를 token fencing 아래 정산한다.
	// inbox와 같은 유한 예산을 사용해 후속 plane의 종료 시간을 남긴다.
	settleCtx, cancel := context.WithTimeout(pgxdb.WithControlAcquire(context.WithoutCancel(ctx)), r.settlementTimeout)
	defer cancel()

	if status == durability.ReplyOutboxAccepted {
		firstCheck := replyHandoffFirstCheck()

		applied, err := r.outbox.SettleAccepted(settleCtx, claim.ID, token, irisRequestID, firstCheck)
		if err != nil {
			return applied, fmt.Errorf("settle accepted: %w", err)
		}

		if applied {
			r.scheduleReplyHandoffWake(firstCheck)
		}

		return applied, nil
	}

	retryAfter := replyOutboxRetryAfterWithBase(status, claim.Attempts, r.outboxRetryAfter)

	out, err := r.outbox.Settle(settleCtx, durability.ReplyOutboxSettlement{
		ID: claim.ID, ClaimToken: token, Status: status, LastError: errorText(dispatchErr), RetryAfter: retryAfter,
	})
	if err != nil {
		return out, fmt.Errorf("settle: %w", err)
	}

	return out, nil
}
