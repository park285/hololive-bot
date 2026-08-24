package botruntime

import (
	"errors"
	"time"

	"github.com/park285/iris-client-go/v2/iris"
	"github.com/park285/shared-go/v2/pkg/backoff"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/bot/orchestration/transport"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/durability"
)

func replyOutboxRetryAfter(status string, attempts int32) time.Duration {
	return replyOutboxRetryAfterWithBase(status, attempts, time.Second)
}

func replyOutboxRetryAfterWithBase(status string, attempts int32, base time.Duration) time.Duration {
	if status != durability.ReplyOutboxRetryablePreDispatch && status != durability.ReplyOutboxOutcomeUnknown {
		return 0
	}

	return backoff.ComputeExponentialBackoff(max(int(attempts)-1, 0), base, time.Minute, base/2)
}

func replyOutboxSettlementStatus(accepted bool, attempts int32, err error) string {
	return replyOutboxSettlementStatusWithMaxAttempts(accepted, attempts, durability.ReplyOutboxMaxAttempts, err)
}

func replyOutboxSettlementStatusWithMaxAttempts(accepted bool, attempts, maxAttempts int32, err error) string {
	if err == nil {
		return durability.ReplyOutboxHandoffCompleted
	}

	if transport.IsReplyStatusFailed(err) {
		return durability.ReplyOutboxDead
	}

	if status, ok := replyUncertainSettlementStatusWithMaxAttempts(accepted, attempts, maxAttempts, err); ok {
		return status
	}

	if errors.Is(err, transport.ErrStoredReplyInvalid) {
		return durability.ReplyOutboxManualReview
	}

	if errors.Is(err, iris.ErrPermanent) {
		return durability.ReplyOutboxPermanentConflict
	}

	if !errors.Is(err, iris.ErrRetryable) || attempts >= maxAttempts {
		return durability.ReplyOutboxDead
	}

	return durability.ReplyOutboxRetryablePreDispatch
}

func replyUncertainSettlementStatusWithMaxAttempts(accepted bool, attempts, maxAttempts int32, err error) (string, bool) {
	if !accepted && !errors.Is(err, transport.ErrReplyOutcomeUnknown) {
		return "", false
	}

	if attempts >= maxAttempts {
		return durability.ReplyOutboxManualReview, true
	}

	return durability.ReplyOutboxOutcomeUnknown, true
}
