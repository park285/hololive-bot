package botruntime

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/park285/iris-client-go/v3/iris"
	"github.com/park285/shared-go/v2/pkg/db/pgxdb"
	"github.com/park285/shared-go/v2/pkg/panicguard"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/durability"
)

const (
	replyHandoffBatchSize = int32(32)
	// 확인 하나(상태 조회와 정산)가 끝나기 전에 같은 행을 다시 고르지 않게 미뤄 두는 soft claim 시간이다. 조회
	// timeout과 정산 예산보다 길어야 하고, 프로세스가 멈추면 이 시간 뒤에 다시 확인한다.
	replyHandoffSoftClaim     = 15 * time.Second
	replyHandoffStatusTimeout = 5 * time.Second
)

// replyHandoffCheckSteps는 봇 accepted 행의 확인 간격이다. 첫 확인은 admission 정산 뒤 첫 간격이고, 이후
// 간격은 accepted_at부터의 경과로 고른다. 마지막 간격을 지평(bot_reply_outbox attempt timeout)까지 반복한다.
// Accepted가 같은 방 후행 응답의 claim을 막으므로(방 순서 보류) 워커 reconciler의 5s 시작 간격 대신 기존
// 250ms 폴링과 같은 지연에서 시작한다.
var replyHandoffCheckSteps = [...]time.Duration{
	250 * time.Millisecond,
	500 * time.Millisecond,
	time.Second,
	2 * time.Second,
	5 * time.Second,
	15 * time.Second,
	time.Minute,
}

// last_error에 남는 reconciler 결과 코드다.
const (
	replyHandoffErrorFailed         = "handoff_failed"
	replyHandoffErrorOutcomeUnknown = "handoff_outcome_unknown"
	replyHandoffErrorStatusMissing  = "handoff_status_missing"
	replyHandoffErrorUnconfirmed    = "handoff_unconfirmed"
)

func replyHandoffFirstCheck() time.Duration {
	return replyHandoffCheckSteps[0]
}

// replyHandoffNextStep은 예정 확인 시각(간격의 누적합) 가운데 elapsed가 아직 닿지 않은 첫 시각의 간격을
// 돌려준다. 확인이 늦게 돌았으면 건너뛴 간격은 다시 쓰지 않는다.
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

// replyHandoffDecision의 status가 비어 있으면 전이 없이 다음 확인으로 미룬다.
type replyHandoffDecision struct {
	status    string
	lastError string
}

// decideReplyHandoff는 Iris /reply-status 관측을 accepted 행의 전이로 바꾼다.
//   - handoff_completed는 확인 완료다.
//   - failed는 Iris가 dispatch 전에 포기한 요청이라 시도 예산과 자동 재발송 지평이 남으면 재발송 큐로
//     되돌린다. 다음 claim의 reissue ladder가 409 CLIENT_REQUEST_ID_FAILED를 받고 :r1/:r2로 올라가며,
//     세대 상한은 ladder가 지킨다. 예산이 없으면 dead다.
//   - outcome_unknown, 모르는 상태, 빈 응답, requestId 불일치, 404는 결과 불명이라 payload를 보존한
//     manual_review다.
//   - 진행 중 상태와 404가 아닌 조회 오류는 지평 전까지 전이 없이 다시 확인하고, 지평에 닿으면
//     handoff 미확인으로 manual_review다.
func decideReplyHandoff(
	claim durability.ReplyHandoffClaim,
	snapshot *iris.ReplyStatusSnapshot,
	queryErr error,
	horizon time.Duration,
	maxAttempts int32,
) replyHandoffDecision {
	if queryErr != nil {
		if httpErr, ok := errors.AsType[*iris.HTTPError](queryErr); ok && httpErr.StatusCode == http.StatusNotFound {
			return replyHandoffDecision{status: durability.ReplyOutboxManualReview, lastError: replyHandoffErrorStatusMissing}
		}

		return pendingReplyHandoffDecision(claim, horizon)
	}

	if snapshot == nil || strings.TrimSpace(snapshot.RequestID) != claim.IrisRequestID {
		return replyHandoffDecision{status: durability.ReplyOutboxManualReview, lastError: replyHandoffErrorOutcomeUnknown}
	}

	// 대소문자·공백 허용은 기존 봇 정책이며 SDK의 정확한 wire 분류와 분리한다.
	switch iris.ClassifyReplyStatus(strings.ToLower(strings.TrimSpace(snapshot.State))) {
	case iris.ReplyStatusHandoffCompleted:
		return replyHandoffDecision{status: durability.ReplyOutboxHandoffCompleted}
	case iris.ReplyStatusFailed:
		retry := !claim.ReplayHorizonExpired &&
			int64(claim.Attempts) < int64(maxAttempts)+int64(claim.OperatorReplayGrants)
		if retry {
			return replyHandoffDecision{status: durability.ReplyOutboxRetryablePreDispatch, lastError: replyHandoffErrorFailed}
		}

		return replyHandoffDecision{status: durability.ReplyOutboxDead, lastError: replyHandoffErrorFailed}
	case iris.ReplyStatusPending:
		return pendingReplyHandoffDecision(claim, horizon)
	case iris.ReplyStatusUnknown:
		// outcome_unknown과 모르는 상태는 아래의 결과 불명 처리로 간다.
	}

	return replyHandoffDecision{status: durability.ReplyOutboxManualReview, lastError: replyHandoffErrorOutcomeUnknown}
}

func pendingReplyHandoffDecision(claim durability.ReplyHandoffClaim, horizon time.Duration) replyHandoffDecision {
	if claim.Elapsed >= horizon {
		return replyHandoffDecision{status: durability.ReplyOutboxManualReview, lastError: replyHandoffErrorUnconfirmed}
	}

	return replyHandoffDecision{}
}

func (r *durableRuntime) runReplyHandoffReconciler(ctx context.Context) {
	runDurableWorker(ctx, "reply_handoff", r.reconcileReplyHandoffBatch, r.handoffWake, r.handoffPollSlot, r.outboxPollEvery)
}

// reconcileReplyHandoffBatch는 확인 시각이 된 accepted 행을 한 batch 고르고, 행마다 Iris 조회와 CAS 정산을
// 병렬로 끝낸 뒤 돌아온다. 고른 행이 없으면 false다.
func (r *durableRuntime) reconcileReplyHandoffBatch(ctx context.Context, _ func()) bool {
	claimCtx, cancel := context.WithTimeout(pgxdb.WithControlAcquire(ctx), r.settlementTimeout)
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
			panicguard.Run(r.logger, panicguard.BackgroundTask, "durable-reply-handoff-check", func() {
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

	// 종료 중 끊긴 조회는 관측이 아니다. Soft claim이 끝나면 다음 프로세스가 다시 확인한다.
	if ctx.Err() != nil {
		return
	}

	decision := decideReplyHandoff(claim, snapshot, queryErr, r.dispatchBudget, r.outboxMaxAttempts)

	settleCtx, cancelSettle := context.WithTimeout(pgxdb.WithControlAcquire(context.WithoutCancel(ctx)), r.settlementTimeout)
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

	// 확인을 끝낸 accepted는 더 이상 같은 방 후행 응답을 막지 않는다. 재발송 대상은 backoff 뒤에 깨운다.
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
