package youtubedispatch

import (
	"log/slog"
	"sync"
	"time"

	"github.com/kapu/hololive-alarm-worker/internal/service/youtube/outbox/dispatchstate"
	"github.com/kapu/hololive-shared/pkg/domain"
)

func (mr *MetricsRecorder) recordGroupedRequestBuildFailure(
	group *deliveryGroup,
	validRows []domain.YouTubeNotificationDelivery,
	validOutboxes []domain.YouTubeNotificationOutbox,
	err error,
	result *dispatchstate.DispatchResult,
	mu *sync.Mutex,
) {
	roomID, channelID, kind := groupedDeliveryFields(group)
	failedAt := time.Now()

	mr.logger.Warn("Failed to build grouped delivery request",
		slog.String("room_id", roomID),
		slog.String("channel_id", channelID),
		slog.String("kind", string(kind)),
		slog.Int("count", len(validOutboxes)),
		dedupeKeyLogAttrForOutboxes(validOutboxes),
		slog.Any("error", err))
	mr.auditLogger.logCommunityShortsDeliveryResult(validRows, validOutboxes, failedAt, "grouped", "failure", "dedupe key")

	for i := range validRows {
		mr.recordDeliveryFailure(result, mu, "dedupe key", validRows[i].ID, validRows[i].OutboxID)
	}
}

func (mr *MetricsRecorder) recordGroupedSendFailure(
	group *deliveryGroup,
	validRows []domain.YouTubeNotificationDelivery,
	validOutboxes []domain.YouTubeNotificationOutbox,
	sendReq deliverySendRequest,
	sendErr error,
	result *dispatchstate.DispatchResult,
	mu *sync.Mutex,
) {
	roomID, channelID, kind := groupedDeliveryFields(group)
	failedAt := time.Now()
	reason := deliveryFailureReason(sendErr)
	mr.logger.Warn("Failed to send grouped delivery",
		slog.String("room_id", roomID),
		slog.String("channel_id", channelID),
		slog.String("kind", string(kind)),
		slog.Int("count", len(validRows)),
		dedupeKeyLogAttr(sendReq.dedupeKeys),
		slog.Any("error", sendErr))
	mr.auditLogger.logCommunityShortsDeliveryResult(validRows, validOutboxes, failedAt, "grouped", "failure", reason)

	for i := range validRows {
		mr.recordDeliveryFailureWithRetryAfter(result, mu, reason, validRows[i].ID, validRows[i].OutboxID, deliveryRetryAfter(sendErr))
	}
}

func (mr *MetricsRecorder) recordGroupedSuccess(
	group *deliveryGroup,
	validRows []domain.YouTubeNotificationDelivery,
	validOutboxes []domain.YouTubeNotificationOutbox,
	sendReq deliverySendRequest,
	claimTokens []dispatchstate.ClaimToken,
	result *dispatchstate.DispatchResult,
	mu *sync.Mutex,
) {
	roomID, channelID, kind := groupedDeliveryFields(group)
	sentAt := time.Now()

	mr.logger.Info("Sent grouped delivery",
		slog.String("room_id", roomID),
		slog.String("channel_id", channelID),
		slog.String("kind", string(kind)),
		slog.Int("count", len(validRows)),
		dedupeKeyLogAttr(sendReq.dedupeKeys))
	mr.auditLogger.logCommunityShortsDeliveryResult(validRows, validOutboxes, sentAt, "grouped", "success", "")

	mu.Lock()

	for i := range validRows {
		result.SuccessDeliveryIDs = append(result.SuccessDeliveryIDs, validRows[i].ID)
		result.TouchedOutboxIDs = append(result.TouchedOutboxIDs, validRows[i].OutboxID)
	}

	result.SuccessClaimTokens = append(result.SuccessClaimTokens, claimTokens...)
	mu.Unlock()
}

func groupedDeliveryFields(group *deliveryGroup) (roomID, channelID string, kind domain.OutboxKind) {
	if group == nil {
		return "", "", ""
	}

	return group.roomID, group.channelID, group.kind
}

// recordGroupedFormatFailure는 묶음 메시지를 만들지 못해 그룹 전체를 재시도 가능한 포맷 실패로 전이한 뒤의 기록이다.
// 개별 발송으로 내려가지 않으므로 묶음의 모든 행을 같은 사유로 남긴다.
func (mr *MetricsRecorder) recordGroupedFormatFailure(
	group *deliveryGroup,
	validRows []domain.YouTubeNotificationDelivery,
	validOutboxes []domain.YouTubeNotificationOutbox,
	err error,
	result *dispatchstate.DispatchResult,
	mu *sync.Mutex,
) {
	roomID, channelID, kind := groupedDeliveryFields(group)
	mr.logger.Warn("Failed to format grouped delivery",
		slog.String("room_id", roomID),
		slog.String("channel_id", channelID),
		slog.String("kind", string(kind)),
		slog.Int("count", len(validOutboxes)),
		slog.Any("error", err))
	mr.auditLogger.logCommunityShortsDeliveryResult(validRows, validOutboxes, time.Now(), "grouped", "failure", "format message")

	for i := range validRows {
		mr.recordDeliveryFailure(result, mu, "format message", validRows[i].ID, validRows[i].OutboxID)
	}
}
