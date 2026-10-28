package botruntime

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/park285/iris-client-go/v4/iris"
	"github.com/park285/shared-go/v2/pkg/retry"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/durability"
)

func (r *durableRuntime) runMaintenance(ctx context.Context) {
	for retry.Sleep(ctx, r.maintenanceEvery) {
		r.maintainDurability(ctx)
	}
}

func (r *durableRuntime) maintainDurability(ctx context.Context) {
	if _, err := r.inbox.ReclaimExpired(ctx, r.inboxMaxAttempts, durableBatchSize); err != nil {
		r.logError("reclaim webhook leases", err)
	}

	reclaim, err := r.outbox.ReclaimExpired(ctx, durableBatchSize)
	if err != nil {
		r.logError("reclaim reply leases", err)
	} else {
		r.observeReplyOutboxReclaim(reclaim)
	}

	r.observeReplyOutboxManualReview(ctx)

	expiredCommands, err := r.commands.ExpireStaleClaims(ctx, commandStaleAfter, durableBatchSize)
	if err != nil {
		r.logError("expire command claims", err)
	} else {
		r.observeExpiredCommands(expiredCommands)
	}

	if _, err := r.ledger.Maintain(ctx, r.terminalRetention, r.manualReviewRetention, durableBatchSize); err != nil {
		r.logError("maintain durable ledger retention", err)
	}
}

func (r *durableRuntime) observeExpiredCommands(expired int64) {
	if expired <= 0 {
		return
	}

	durableCommandOutcomeUnknownTotal.Add(float64(expired))

	if r.logger != nil {
		r.logger.Error("durable command claims expired with unknown outcomes",
			slog.Int64("rows", expired),
			slog.String("action", "inspect bot_command_executions status=outcome_unknown"))
	}
}

func (r *durableRuntime) observeReplyOutboxReclaim(reclaim durability.ReplyOutboxReclaim) {
	if reclaim.SafetyManualReview <= 0 {
		return
	}

	if r.logger != nil {
		r.logger.Error("reply leases moved to manual review",
			slog.Int64("rows", reclaim.SafetyManualReview),
			slog.String("action", "inspect bot_reply_outbox status=manual_review before replay decision"))
	}
}

func (r *durableRuntime) observeReplyOutboxManualReview(ctx context.Context) {
	stats, err := r.outbox.ManualReviewStats(ctx)
	if err != nil {
		replyOutboxManualReviewObservationFailuresTotal.Inc()
		// 이전 건수·경과를 0으로 덮지 않고 유효성만 내린다. 마지막 성공 시각도 유지해 관측 공백을 드러낸다.
		replyOutboxManualReviewSnapshotSuccess.Set(0)
		r.logError("observe reply outbox manual review backlog", err)

		return
	}

	replyOutboxManualReviewBacklog.Set(float64(stats.Backlog))
	replyOutboxManualReviewOldestAgeSeconds.Set(stats.OldestAge.Seconds())
	replyOutboxManualReviewSnapshotLastSuccess.Set(float64(r.nowTime().Unix()))
	replyOutboxManualReviewSnapshotSuccess.Set(1)
}

func (r *durableRuntime) logError(message string, err error) {
	logDurableError(r.logger, message, err)
}

func logDurableError(logger *slog.Logger, message string, err error) {
	if logger == nil || err == nil {
		return
	}

	if status, ok := irisHTTPStatus(err); ok {
		logger.Error(message, slog.Int("http_status", status))

		return
	}

	if attrs, ok := safeErrorLogAttrs(err); ok {
		logger.Error(message, attrs...)

		return
	}

	logger.Error(message, slog.String("reason", runtimeErrorReason(err)))
}

func irisHTTPStatus(err error) (int, bool) {
	httpErr, ok := errors.AsType[*iris.HTTPError](err)
	if !ok {
		return 0, false
	}

	return httpErr.StatusCode, true
}

type safeLoggableError interface {
	error
	SafeMessageToken() string
	SafeReason() string
}

func safeErrorLogAttrs(err error) ([]any, bool) {
	if safeErr, ok := errors.AsType[safeLoggableError](err); ok {
		attrs := []any{slog.String("reason", safeErr.SafeReason())}
		if token := safeErr.SafeMessageToken(); token != "" {
			attrs = append(attrs, slog.String("message_token", token))
		}

		return attrs, true
	}

	// durability의 ErrInvalidArgument 계열은 위반된 필드명과 제약만 담고 메시지 본문·방·발신자 값은
	// 담지 않는다. 그 불변식 덕분에 원문을 그대로 로그에 실어도 개인정보가 새지 않는다.
	if errors.Is(err, durability.ErrInvalidArgument) {
		return []any{
			slog.String("reason", "invalid_argument"),
			slog.String("constraint", err.Error()),
		}, true
	}

	return nil, false
}

func runtimeErrorReason(err error) string {
	switch {
	case errors.Is(err, context.Canceled):
		return "context_canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return "context_deadline_exceeded"
	default:
		return "operation_failed"
	}
}

func errorText(err error) string {
	if err == nil {
		return ""
	}

	if status, ok := irisHTTPStatus(err); ok {
		return fmt.Sprintf("iris http status=%d", status)
	}

	return dispatchErrorReason(err)
}

func dispatchErrorReason(err error) string {
	switch {
	case errors.Is(err, context.Canceled):
		return "dispatch_context_canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return "dispatch_context_deadline_exceeded"
	case errors.Is(err, iris.ErrRetryable):
		return "iris_retryable"
	default:
		return "dispatch_failed"
	}
}

func nextDurableIdleDelayFrom(current, minimum time.Duration) time.Duration {
	if current < minimum {
		return minimum
	}

	return min(current*2, max(durablePollMax, minimum))
}

func waitDurableWake(ctx context.Context, delay time.Duration, wake <-chan struct{}) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return false
	case <-wake:
		return true
	case <-timer.C:
		return true
	}
}

func notifyDurable(wake chan struct{}) {
	select {
	case wake <- struct{}{}:
	default:
	}
}
