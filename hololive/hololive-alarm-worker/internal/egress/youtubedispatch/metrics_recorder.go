package youtubedispatch

import (
	"log/slog"
)

// MetricsRecorder는 확정된 발송 결과를 로그·audit·DispatchResult에 기록하며, claim 해제 같은 상태 변경은 하지 않는다.
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
