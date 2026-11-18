package youtubedispatch

import (
	"log/slog"
)

type MetricsRecorder struct {
	logger      *slog.Logger
	auditLogger *AuditLogger
}

func newMetricsRecorder(logger *slog.Logger, auditLogger *AuditLogger) *MetricsRecorder {
	return &MetricsRecorder{logger: logger, auditLogger: auditLogger}
}

func (mr *MetricsRecorder) recordDeliveryFailure(result *deliveryDispatchResult, outboxID int64) {
	result.mu.Lock()

	result.FailedDeliveries++

	result.TouchedOutboxIDs = append(result.TouchedOutboxIDs, outboxID)
	result.mu.Unlock()
}
