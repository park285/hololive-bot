package youtubedispatch

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/dispatchstate"
)

type MetricsRecorder struct {
	claimReleaser

	logger      *slog.Logger
	auditLogger *AuditLogger
}

type claimReleaser interface {
	releaseDeliveryClaimsWithWarning(ctx context.Context, claims []dispatchstate.ClaimToken, message string, attrs ...any)
}

func newMetricsRecorder(logger *slog.Logger, auditLogger *AuditLogger, cr claimReleaser) *MetricsRecorder {
	return &MetricsRecorder{
		logger:        logger,
		auditLogger:   auditLogger,
		claimReleaser: cr,
	}
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
