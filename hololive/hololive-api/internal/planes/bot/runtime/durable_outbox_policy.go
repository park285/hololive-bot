package botruntime

import (
	"errors"
	"time"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/bot/orchestration/transport"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/durability"
	"github.com/park285/iris-client-go/iris"
	"github.com/park285/shared-go/pkg/backoff"
)

func replyOutboxRetryAfter(status string, attempts int32) time.Duration {
	if status != durability.ReplyOutboxRetryablePreDispatch && status != durability.ReplyOutboxOutcomeUnknown {
		return 0
	}
	return backoff.ComputeExponentialBackoff(max(int(attempts)-1, 0), time.Second, time.Minute, 500*time.Millisecond)
}

func replyOutboxSettlementStatus(accepted bool, attempts int32, err error) string {
	if err == nil {
		return durability.ReplyOutboxHandoffCompleted
	}
	if transport.IsReplyStatusFailed(err) {
		return durability.ReplyOutboxDead
	}
	if status, ok := replyUncertainSettlementStatus(accepted, attempts, err); ok {
		return status
	}
	if errors.Is(err, transport.ErrStoredReplyInvalid) {
		return durability.ReplyOutboxManualReview
	}
	if errors.Is(err, iris.ErrPermanent) {
		return durability.ReplyOutboxPermanentConflict
	}
	if !errors.Is(err, iris.ErrRetryable) || attempts >= durability.ReplyOutboxMaxAttempts {
		return durability.ReplyOutboxDead
	}
	return durability.ReplyOutboxRetryablePreDispatch
}

func replyUncertainSettlementStatus(accepted bool, attempts int32, err error) (string, bool) {
	if !accepted && !errors.Is(err, transport.ErrReplyOutcomeUnknown) {
		return "", false
	}
	if attempts >= durability.ReplyOutboxMaxAttempts {
		return durability.ReplyOutboxManualReview, true
	}
	return durability.ReplyOutboxOutcomeUnknown, true
}
