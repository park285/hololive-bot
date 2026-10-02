package botruntime

import (
	"errors"
	"time"

	"github.com/park285/iris-client-go/v3/iris"
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
	// Iris가 수리한 응답은 자동 재발송 큐로 돌리지 않는다. 관측 실패도 즉시 정산해
	// lease 만료까지 같은 방의 후속 응답을 막지 않고 수동 확인 대상으로 남긴다.
	if accepted {
		return durability.ReplyOutboxManualReview, true
	}

	if !transport.IsReplyOutcomeUnknown(err) {
		return "", false
	}

	if attempts >= maxAttempts {
		return durability.ReplyOutboxManualReview, true
	}

	return durability.ReplyOutboxOutcomeUnknown, true
}
