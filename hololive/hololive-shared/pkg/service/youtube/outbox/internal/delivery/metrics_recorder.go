package delivery

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

type MetricsRecorder struct {
	logger      *slog.Logger
	auditLogger *AuditLogger
	claimReleaser
}

type claimReleaser interface {
	releaseDeliveryClaimsWithWarning(ctx context.Context, claims []deliveryClaimToken, message string, attrs ...any)
}

func newMetricsRecorder(logger *slog.Logger, auditLogger *AuditLogger, cr claimReleaser) *MetricsRecorder {
	return &MetricsRecorder{
		logger:        logger,
		auditLogger:   auditLogger,
		claimReleaser: cr,
	}
}

func (mr *MetricsRecorder) recordPerRoomFormatFailure(
	ctx context.Context,
	row domain.YouTubeNotificationDelivery,
	rows []domain.YouTubeNotificationDelivery,
	outboxes []domain.YouTubeNotificationOutbox,
	claimTokens []deliveryClaimToken,
	result *deliveryDispatchResult,
	mu *sync.Mutex,
) {
	mr.releaseDeliveryClaimsWithWarning(ctx, claimTokens, "Failed to release per-room delivery claims after format error",
		slog.Int64("delivery_id", row.ID),
		slog.Int64("outbox_id", row.OutboxID),
	)
	failedAt := time.Now()
	mr.auditLogger.logCommunityShortsDeliveryAudit(ctx, rows, outboxes, failedAt, "per_room", "failure", "format message", nil)
	mr.auditLogger.logCommunityShortsDeliveryResult(rows, outboxes, failedAt, "per_room", "failure", "format message")
	mr.recordDeliveryFailure(result, mu, "format message", row.ID, row.OutboxID)
}

func (mr *MetricsRecorder) recordPerRoomMissingMessage(
	ctx context.Context,
	row domain.YouTubeNotificationDelivery,
	claimTokens []deliveryClaimToken,
	result *deliveryDispatchResult,
	mu *sync.Mutex,
) {
	mr.releaseDeliveryClaimsWithWarning(ctx, claimTokens, "Failed to release per-room delivery claims after missing preformatted message",
		slog.Int64("delivery_id", row.ID),
		slog.Int64("outbox_id", row.OutboxID),
	)
	mr.recordDeliveryFailure(result, mu, "outbox row not found", row.ID, row.OutboxID)
}

func (mr *MetricsRecorder) recordPerRoomRequestBuildFailure(
	ctx context.Context,
	row domain.YouTubeNotificationDelivery,
	outbox domain.YouTubeNotificationOutbox,
	rows []domain.YouTubeNotificationDelivery,
	outboxes []domain.YouTubeNotificationOutbox,
	claimTokens []deliveryClaimToken,
	err error,
	result *deliveryDispatchResult,
	mu *sync.Mutex,
) {
	mr.releaseDeliveryClaimsWithWarning(ctx, claimTokens, "Failed to release per-room delivery claims after request build error",
		slog.Int64("delivery_id", row.ID),
		slog.Int64("outbox_id", row.OutboxID),
	)
	failedAt := time.Now()
	mr.logger.Warn("Failed to build per-room delivery request",
		slog.Int64("delivery_id", row.ID),
		slog.Int64("outbox_id", row.OutboxID),
		slog.String("room_id", row.RoomID),
		dedupeKeyLogAttrForOutboxes([]domain.YouTubeNotificationOutbox{outbox}),
		slog.Any("error", err))
	mr.auditLogger.logCommunityShortsDeliveryAudit(ctx, rows, outboxes, failedAt, "per_room", "failure", "dedupe key", err)
	mr.auditLogger.logCommunityShortsDeliveryResult(rows, outboxes, failedAt, "per_room", "failure", "dedupe key")
	mr.recordDeliveryFailure(result, mu, "dedupe key", row.ID, row.OutboxID)
}

func (mr *MetricsRecorder) recordPerRoomSendFailure(
	ctx context.Context,
	row domain.YouTubeNotificationDelivery,
	rows []domain.YouTubeNotificationDelivery,
	outboxes []domain.YouTubeNotificationOutbox,
	sendReq deliverySendRequest,
	claimTokens []deliveryClaimToken,
	sendErr error,
	result *deliveryDispatchResult,
	mu *sync.Mutex,
) {
	mr.releaseDeliveryClaimsWithWarning(ctx, claimTokens, "Failed to release per-room delivery claims after send failure",
		slog.Int64("delivery_id", row.ID),
		slog.Int64("outbox_id", row.OutboxID),
	)
	failedAt := time.Now()
	reason := deliveryFailureReason(sendErr)
	mr.logger.Warn("Failed to send per-room delivery",
		slog.Int64("delivery_id", row.ID),
		slog.Int64("outbox_id", row.OutboxID),
		slog.String("room_id", row.RoomID),
		dedupeKeyLogAttr(sendReq.dedupeKeys),
		slog.Any("error", sendErr))
	mr.auditLogger.logCommunityShortsDeliveryAudit(ctx, rows, outboxes, failedAt, "per_room", "failure", reason, sendErr)
	mr.auditLogger.logCommunityShortsDeliveryResult(rows, outboxes, failedAt, "per_room", "failure", reason)
	mr.recordDeliveryFailure(result, mu, reason, row.ID, row.OutboxID)
}

func (mr *MetricsRecorder) recordPerRoomSuccess(
	ctx context.Context,
	row domain.YouTubeNotificationDelivery,
	rows []domain.YouTubeNotificationDelivery,
	outboxes []domain.YouTubeNotificationOutbox,
	sendReq deliverySendRequest,
	claimTokens []deliveryClaimToken,
	result *deliveryDispatchResult,
	mu *sync.Mutex,
) {
	sentAt := time.Now()
	mr.logger.Info("Sent per-room delivery",
		slog.Int64("delivery_id", row.ID),
		slog.Int64("outbox_id", row.OutboxID),
		slog.String("room_id", row.RoomID),
		dedupeKeyLogAttr(sendReq.dedupeKeys))
	mr.auditLogger.logCommunityShortsDeliveryAudit(ctx, rows, outboxes, sentAt, "per_room", "success", "", nil)
	mr.auditLogger.logCommunityShortsDeliveryResult(rows, outboxes, sentAt, "per_room", "success", "")

	mu.Lock()
	result.successDeliveryIDs = append(result.successDeliveryIDs, row.ID)
	result.touchedOutboxIDs = append(result.touchedOutboxIDs, row.OutboxID)
	result.successClaimTokens = append(result.successClaimTokens, claimTokens...)
	mu.Unlock()
}

func (mr *MetricsRecorder) recordDeliveryFailure(
	result *deliveryDispatchResult,
	mu *sync.Mutex,
	reason string,
	deliveryID, outboxID int64,
) {
	mu.Lock()
	result.failedDeliveries++
	result.failureBuckets[reason] = append(result.failureBuckets[reason], deliveryID)
	result.touchedOutboxIDs = append(result.touchedOutboxIDs, outboxID)
	mu.Unlock()
}

func (mr *MetricsRecorder) recordGroupedRequestBuildFailure(
	ctx context.Context,
	group deliveryGroup,
	validRows []domain.YouTubeNotificationDelivery,
	validOutboxes []domain.YouTubeNotificationOutbox,
	claimTokens []deliveryClaimToken,
	err error,
	result *deliveryDispatchResult,
	mu *sync.Mutex,
) {
	mr.releaseDeliveryClaimsWithWarning(ctx, claimTokens, "Failed to release grouped delivery claims after request build error",
		slog.String("room_id", group.roomID),
		slog.String("channel_id", group.channelID),
	)
	failedAt := time.Now()
	mr.logger.Warn("Failed to build grouped delivery request",
		slog.String("room_id", group.roomID),
		slog.String("channel_id", group.channelID),
		slog.String("kind", string(group.kind)),
		slog.Int("count", len(validOutboxes)),
		dedupeKeyLogAttrForOutboxes(validOutboxes),
		slog.Any("error", err))
	mr.auditLogger.logCommunityShortsDeliveryAudit(ctx, validRows, validOutboxes, failedAt, "grouped", "failure", "dedupe key", err)
	mr.auditLogger.logCommunityShortsDeliveryResult(validRows, validOutboxes, failedAt, "grouped", "failure", "dedupe key")
	for i := range validRows {
		mr.recordDeliveryFailure(result, mu, "dedupe key", validRows[i].ID, validRows[i].OutboxID)
	}
}

func (mr *MetricsRecorder) recordGroupedSendFailure(
	ctx context.Context,
	group deliveryGroup,
	validRows []domain.YouTubeNotificationDelivery,
	validOutboxes []domain.YouTubeNotificationOutbox,
	sendReq deliverySendRequest,
	claimTokens []deliveryClaimToken,
	sendErr error,
	result *deliveryDispatchResult,
	mu *sync.Mutex,
) {
	mr.releaseDeliveryClaimsWithWarning(ctx, claimTokens, "Failed to release grouped delivery claims after send failure",
		slog.String("room_id", group.roomID),
		slog.String("channel_id", group.channelID),
	)
	failedAt := time.Now()
	reason := deliveryFailureReason(sendErr)
	mr.logger.Warn("Failed to send grouped delivery",
		slog.String("room_id", group.roomID),
		slog.String("kind", string(group.kind)),
		slog.Int("count", len(validRows)),
		dedupeKeyLogAttr(sendReq.dedupeKeys),
		slog.Any("error", sendErr))
	mr.auditLogger.logCommunityShortsDeliveryAudit(ctx, validRows, validOutboxes, failedAt, "grouped", "failure", reason, sendErr)
	mr.auditLogger.logCommunityShortsDeliveryResult(validRows, validOutboxes, failedAt, "grouped", "failure", reason)
	for i := range validRows {
		mr.recordDeliveryFailure(result, mu, reason, validRows[i].ID, validRows[i].OutboxID)
	}
}

func (mr *MetricsRecorder) recordGroupedSuccess(
	ctx context.Context,
	group deliveryGroup,
	validRows []domain.YouTubeNotificationDelivery,
	validOutboxes []domain.YouTubeNotificationOutbox,
	sendReq deliverySendRequest,
	claimTokens []deliveryClaimToken,
	result *deliveryDispatchResult,
	mu *sync.Mutex,
) {
	sentAt := time.Now()
	mr.logger.Info("Sent grouped delivery",
		slog.String("room_id", group.roomID),
		slog.String("channel_id", group.channelID),
		slog.String("kind", string(group.kind)),
		slog.Int("count", len(validRows)),
		dedupeKeyLogAttr(sendReq.dedupeKeys))
	mr.auditLogger.logCommunityShortsDeliveryAudit(ctx, validRows, validOutboxes, sentAt, "grouped", "success", "", nil)
	mr.auditLogger.logCommunityShortsDeliveryResult(validRows, validOutboxes, sentAt, "grouped", "success", "")

	mu.Lock()
	for i := range validRows {
		result.successDeliveryIDs = append(result.successDeliveryIDs, validRows[i].ID)
		result.touchedOutboxIDs = append(result.touchedOutboxIDs, validRows[i].OutboxID)
	}
	result.successClaimTokens = append(result.successClaimTokens, claimTokens...)
	mu.Unlock()
}

func (mr *MetricsRecorder) recordKaringRequestBuildFailure(
	ctx context.Context,
	roomID string,
	channelID string,
	kind domain.OutboxKind,
	rows []domain.YouTubeNotificationDelivery,
	outboxes []domain.YouTubeNotificationOutbox,
	claimTokens []deliveryClaimToken,
	mode string,
	err error,
	result *deliveryDispatchResult,
	mu *sync.Mutex,
) {
	mr.releaseDeliveryClaimsWithWarning(ctx, claimTokens, "Failed to release Karing delivery claims after request build error",
		slog.String("room_id", roomID),
		slog.String("channel_id", channelID),
	)
	failedAt := time.Now()
	mr.logger.Warn("Failed to build Karing delivery request",
		slog.String("room_id", roomID),
		slog.String("channel_id", channelID),
		slog.String("kind", string(kind)),
		slog.Int("count", len(outboxes)),
		dedupeKeyLogAttrForOutboxes(outboxes),
		slog.Any("error", err))
	mr.auditLogger.logCommunityShortsDeliveryAudit(ctx, rows, outboxes, failedAt, mode, "failure", "karing request", err)
	mr.auditLogger.logCommunityShortsDeliveryResult(rows, outboxes, failedAt, mode, "failure", "karing request")
	for i := range rows {
		mr.recordDeliveryFailure(result, mu, "karing request", rows[i].ID, rows[i].OutboxID)
	}
}

func (mr *MetricsRecorder) recordKaringSendFailure(
	ctx context.Context,
	roomID string,
	channelID string,
	kind domain.OutboxKind,
	rows []domain.YouTubeNotificationDelivery,
	outboxes []domain.YouTubeNotificationOutbox,
	sendReq deliverySendRequest,
	claimTokens []deliveryClaimToken,
	mode string,
	err error,
	result *deliveryDispatchResult,
	mu *sync.Mutex,
) {
	mr.releaseDeliveryClaimsWithWarning(ctx, claimTokens, "Failed to release Karing delivery claims after send failure",
		slog.String("room_id", roomID),
		slog.String("channel_id", channelID),
	)
	failedAt := time.Now()
	mr.logger.Warn("Failed to send Karing delivery",
		slog.String("room_id", roomID),
		slog.String("channel_id", channelID),
		slog.String("kind", string(kind)),
		slog.Int("count", len(rows)),
		dedupeKeyLogAttr(sendReq.dedupeKeys),
		slog.Any("error", err))
	mr.auditLogger.logCommunityShortsDeliveryAudit(ctx, rows, outboxes, failedAt, mode, "failure", "karing send", err)
	mr.auditLogger.logCommunityShortsDeliveryResult(rows, outboxes, failedAt, mode, "failure", "karing send")
	for i := range rows {
		mr.recordDeliveryFailure(result, mu, "karing send", rows[i].ID, rows[i].OutboxID)
	}
}

func (mr *MetricsRecorder) recordKaringSuccess(
	ctx context.Context,
	roomID string,
	channelID string,
	kind domain.OutboxKind,
	rows []domain.YouTubeNotificationDelivery,
	outboxes []domain.YouTubeNotificationOutbox,
	sendReq deliverySendRequest,
	claimTokens []deliveryClaimToken,
	mode string,
	result *deliveryDispatchResult,
	mu *sync.Mutex,
) {
	sentAt := time.Now()
	mr.logger.Info("Sent Karing delivery",
		slog.String("room_id", roomID),
		slog.String("channel_id", channelID),
		slog.String("kind", string(kind)),
		slog.String("delivery_mode", mode),
		slog.Int("count", len(rows)),
		dedupeKeyLogAttr(sendReq.dedupeKeys))
	mr.auditLogger.logCommunityShortsDeliveryAudit(ctx, rows, outboxes, sentAt, mode, "success", "", nil)
	mr.auditLogger.logCommunityShortsDeliveryResult(rows, outboxes, sentAt, mode, "success", "")

	mu.Lock()
	for i := range rows {
		result.successDeliveryIDs = append(result.successDeliveryIDs, rows[i].ID)
		result.touchedOutboxIDs = append(result.touchedOutboxIDs, rows[i].OutboxID)
	}
	result.successClaimTokens = append(result.successClaimTokens, claimTokens...)
	mu.Unlock()
}
