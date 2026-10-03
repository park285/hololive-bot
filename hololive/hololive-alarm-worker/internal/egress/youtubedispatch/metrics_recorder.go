package youtubedispatch

import (
	"log/slog"
	"sync"
	"time"

	"github.com/kapu/hololive-alarm-worker/internal/service/youtube/outbox/dispatchstate"
)

// MetricsRecorder는 확정된 발송 결과를 로그·audit·DispatchResult에 기록하며, claim 해제 같은 상태 변경은 하지 않는다.
type MetricsRecorder struct {
	logger      *slog.Logger
	auditLogger *AuditLogger
}

func newMetricsRecorder(logger *slog.Logger, auditLogger *AuditLogger) *MetricsRecorder {
	return &MetricsRecorder{logger: logger, auditLogger: auditLogger}
}

func (mr *MetricsRecorder) recordDeliveryFailure(
	result *dispatchstate.DispatchResult,
	mu *sync.Mutex,
	reason string,
	deliveryID, outboxID int64,
) {
	mr.recordDeliveryFailureWithRetryAfter(result, mu, reason, deliveryID, outboxID, 0)
}

func (mr *MetricsRecorder) recordDeliveryFailureWithRetryAfter(
	result *dispatchstate.DispatchResult,
	mu *sync.Mutex,
	reason string,
	deliveryID, outboxID int64,
	retryAfter time.Duration,
) {
	mu.Lock()

	result.FailedDeliveries++
	if result.FailureBuckets == nil {
		result.FailureBuckets = make(map[string][]int64)
	}

	result.FailureBuckets[reason] = append(result.FailureBuckets[reason], deliveryID)

	if retryAfter > 0 {
		if result.FailureRetryAfter == nil {
			result.FailureRetryAfter = make(map[string]time.Duration)
		}

		if retryAfter > result.FailureRetryAfter[reason] {
			result.FailureRetryAfter[reason] = retryAfter
		}
	}

	result.TouchedOutboxIDs = append(result.TouchedOutboxIDs, outboxID)
	mu.Unlock()
}
