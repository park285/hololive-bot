package botruntime

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/park285/iris-client-go/v6/iris"
	"github.com/park285/shared-go/v2/pkg/panicguard"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/durability"
	contractsalarm "github.com/kapu/hololive-shared/pkg/contracts/alarm"
	"github.com/kapu/hololive-shared/pkg/panicscope"
	"github.com/kapu/hololive-shared/pkg/service/delivery"
)

const (
	replyHandoffBatchSize     = int32(32)
	replyHandoffSoftClaim     = 15 * time.Second
	replyHandoffStatusTimeout = 5 * time.Second
)

var replyHandoffCheckSteps = [...]time.Duration{
	250 * time.Millisecond,
	500 * time.Millisecond,
	time.Second,
	2 * time.Second,
	5 * time.Second,
	15 * time.Second,
	time.Minute,
}

func replyHandoffFirstCheck() time.Duration {
	return replyHandoffCheckSteps[0]
}

func replyHandoffNextStep(elapsed time.Duration) time.Duration {
	var offset time.Duration

	for _, step := range replyHandoffCheckSteps {
		offset += step
		if elapsed < offset {
			return step
		}
	}

	return replyHandoffCheckSteps[len(replyHandoffCheckSteps)-1]
}

type replyHandoffDecision struct {
	status    string
	lastError string
}

func decideReplyHandoff(
	claim durability.ReplyHandoffClaim,
	snapshot *iris.ReplyStatusSnapshot,
	queryErr error,
	horizon time.Duration,
	maxAttempts int32,
) replyHandoffDecision {
	if queryErr != nil {
		if httpErr, ok := errors.AsType[*iris.HTTPError](queryErr); ok && httpErr.StatusCode == http.StatusNotFound {
			return replyHandoffDecision{status: durability.ReplyOutboxManualReview, lastError: contractsalarm.HandoffCodeStatusMissing}
		}

		if delivery.ReplyStatusIdentityMismatch(queryErr) {
			return replyHandoffDecision{status: durability.ReplyOutboxManualReview, lastError: contractsalarm.HandoffCodeOutcomeUnknown}
		}

		return pendingReplyHandoffDecision(claim, horizon)
	}

	if snapshot == nil || snapshot.RequestID != claim.IrisRequestID {
		return replyHandoffDecision{status: durability.ReplyOutboxManualReview, lastError: contractsalarm.HandoffCodeOutcomeUnknown}
	}

	switch iris.ClassifyReplyStatus(snapshot.State) {
	case iris.ReplyStatusHandoffCompleted:
		return replyHandoffDecision{status: durability.ReplyOutboxHandoffCompleted}
	case iris.ReplyStatusFailed:
		if iris.ReplyStatusRoomNotJoined(snapshot) {
			return replyHandoffDecision{status: durability.ReplyOutboxDead, lastError: contractsalarm.HandoffCodeRoomNotJoined}
		}

		retry := !claim.ReplayHorizonExpired &&
			int64(claim.Attempts) < int64(maxAttempts)+int64(claim.OperatorReplayGrants)
		if retry {
			return replyHandoffDecision{status: durability.ReplyOutboxRetryablePreDispatch, lastError: contractsalarm.HandoffCodeFailed}
		}

		return replyHandoffDecision{status: durability.ReplyOutboxDead, lastError: contractsalarm.HandoffCodeFailed}
	case iris.ReplyStatusPending:
		return pendingReplyHandoffDecision(claim, horizon)
	case iris.ReplyStatusUnknown:
	}

	return replyHandoffDecision{status: durability.ReplyOutboxManualReview, lastError: contractsalarm.HandoffCodeOutcomeUnknown}
}

func pendingReplyHandoffDecision(claim durability.ReplyHandoffClaim, horizon time.Duration) replyHandoffDecision {
	if claim.Elapsed >= horizon {
		return replyHandoffDecision{status: durability.ReplyOutboxManualReview, lastError: contractsalarm.HandoffCodeUnconfirmed}
	}

	return replyHandoffDecision{}
}

func (r *durableRuntime) runReplyHandoffReconciler(ctx context.Context) {
	runDurableWorker(ctx, "reply_handoff", r.reconcileReplyHandoffBatch, r.handoffWake, r.handoffPollSlot, r.outboxPollEvery)
}

func (r *durableRuntime) reconcileReplyHandoffBatch(ctx context.Context, _ func()) bool {
	claimCtx, cancel := context.WithTimeout(ctx, r.settlementTimeout)
	claims, err := r.outbox.ClaimHandoffDue(claimCtx, replyHandoffBatchSize, replyHandoffSoftClaim)

	cancel()

	if err != nil {
		r.logError("claim reply handoff checks", err)

		return false
	}

	if len(claims) == 0 {
		return false
	}

	var wg sync.WaitGroup

	for _, claim := range claims {
		wg.Go(func() {
			panicguard.Run(r.logger, panicscope.BackgroundTask, "durable-reply-handoff-check", func() {
				r.reconcileReplyHandoff(ctx, claim)
			})
		})
	}

	wg.Wait()

	return true
}

func (r *durableRuntime) reconcileReplyHandoff(ctx context.Context, claim durability.ReplyHandoffClaim) {
	statusCtx, cancel := context.WithTimeout(ctx, replyHandoffStatusTimeout)
	snapshot, queryErr := r.irisClient.GetReplyStatus(statusCtx, claim.IrisRequestID)

	cancel()

	if ctx.Err() != nil {
		return
	}

	decision := decideReplyHandoff(claim, snapshot, queryErr, r.dispatchBudget, r.outboxMaxAttempts)

	settleCtx, cancelSettle := context.WithTimeout(context.WithoutCancel(ctx), r.settlementTimeout)
	defer cancelSettle()

	if decision.status == "" {
		r.rescheduleReplyHandoff(settleCtx, claim, queryErr)

		return
	}

	retryAfter := replyOutboxRetryAfterWithBase(decision.status, claim.Attempts, r.outboxRetryAfter)

	applied, err := r.outbox.SettleHandoff(settleCtx, durability.ReplyHandoffSettlement{
		Claim: claim, Status: decision.status, LastError: decision.lastError, RetryAfter: retryAfter,
	})
	if err != nil {
		r.logError("settle reply handoff", err)

		return
	}

	if !applied {
		r.logReplyHandoffFenceMiss()

		return
	}

	r.observeReplyHandoffSettlement(claim, decision)

	if decision.status == durability.ReplyOutboxRetryablePreDispatch {
		time.AfterFunc(retryAfter, func() { notifyDurable(r.outboxWake) })

		return
	}

	notifyDurable(r.outboxWake)
}

func (r *durableRuntime) rescheduleReplyHandoff(ctx context.Context, claim durability.ReplyHandoffClaim, queryErr error) {
	if queryErr != nil && r.logger != nil {
		r.logger.Warn("reply handoff status query deferred", replyHandoffQueryErrorAttrs(queryErr)...)
	}

	delay, applied, err := r.outbox.RescheduleHandoff(ctx, claim, replyHandoffNextStep(claim.Elapsed), r.dispatchBudget)
	if err != nil {
		r.logError("reschedule reply handoff", err)

		return
	}

	if !applied {
		r.logReplyHandoffFenceMiss()

		return
	}

	r.scheduleReplyHandoffWake(delay)
}

func (r *durableRuntime) scheduleReplyHandoffWake(delay time.Duration) {
	time.AfterFunc(delay, func() { notifyDurable(r.handoffWake) })
}

func (r *durableRuntime) logReplyHandoffFenceMiss() {
	if r.logger != nil {
		r.logger.Warn("reply handoff check lost its claim; the next check decides again")
	}
}

func (r *durableRuntime) observeReplyHandoffSettlement(claim durability.ReplyHandoffClaim, decision replyHandoffDecision) {
	if r.logger == nil || decision.status == durability.ReplyOutboxHandoffCompleted {
		return
	}

	attrs := []any{
		slog.String("status", decision.status),
		slog.String("reason", decision.lastError),
		slog.Int("attempts", int(claim.Attempts)),
	}
	if decision.status == durability.ReplyOutboxRetryablePreDispatch {
		r.logger.Warn("reply handoff failed; requeued for reissue", attrs...)

		return
	}

	r.logger.Error("reply handoff reached non-success terminal state", attrs...)
}

func replyHandoffQueryErrorAttrs(err error) []any {
	if status, ok := irisHTTPStatus(err); ok {
		return []any{slog.Int("http_status", status)}
	}

	return []any{slog.String("reason", runtimeErrorReason(err))}
}
