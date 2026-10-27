package botruntime

import (
	"errors"
	"time"

	"github.com/park285/iris-client-go/v4/iris"
	"github.com/park285/shared-go/v2/pkg/backoff"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/bot/orchestration/transport"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/durability"
)

func replyOutboxRetryAfterWithBase(status string, attempts int32, base time.Duration) time.Duration {
	if status != durability.ReplyOutboxRetryablePreDispatch && status != durability.ReplyOutboxOutcomeUnknown {
		return 0
	}

	return backoff.ComputeExponentialBackoff(max(int(attempts)-1, 0), base, time.Minute, base/2)
}

// replyOutboxSettlementStatusWithMaxAttempts는 한 번의 admission 시도 결과를 outbox 상태로 바꾼다. 오류가
// 없으면 Iris가 202와 requestId로 durable admission을 증명한 것이고 handoff는 reconciler가 확인한다.
func replyOutboxSettlementStatusWithMaxAttempts(attempts, maxAttempts int32, err error) string {
	if err == nil {
		return durability.ReplyOutboxAccepted
	}

	if transport.IsReplyOutcomeUnknown(err) {
		if attempts >= maxAttempts {
			return durability.ReplyOutboxManualReview
		}

		return durability.ReplyOutboxOutcomeUnknown
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
