package youtubedispatch

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/park285/shared-go/v2/pkg/runtime/lifecycle"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/privacylog"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/telemetry"
	timeline "github.com/kapu/hololive-shared/pkg/service/youtube/outbox/timeline"
)

func buildDeliveryAuditLogAttrsWithClassification(row *domain.YouTubeNotificationDeliveryTelemetry, classification *timeline.PostLatencyClassificationResult) []any {
	attrs := []any{
		slog.Int64(FieldDeliveryID, row.DeliveryID),
		slog.Int64(FieldOutboxID, row.OutboxID),
		privacylog.RoomIDAttr(row.RoomID),
		slog.String(FieldChannelID, row.ChannelID),
		slog.String(FieldPostID, row.PostID),
		slog.String(FieldContentID, strings.TrimSpace(row.ContentID)),
		slog.String(FieldAlarmType, string(row.AlarmType)),
		slog.Time(FieldSentAt, row.EventAt.UTC()),
		slog.String(FieldSendResult, row.SendResult),
		slog.String(FieldDeliveryPath, strings.TrimSpace(row.DeliveryPath)),
		slog.String(FieldDeliveryMode, row.DeliveryMode),
		slog.String(FieldDedupeKey, row.DedupeKey),
		slog.Int(FieldAttemptOrdinal, row.AttemptOrdinal),
	}

	attrs = appendCommunityShortsAlarmTimingLogAttrs(attrs, telemetry.CommunityShortsAlarmTimingForTelemetryRow(row))

	if row.DetectedAt != nil {
		attrs = append(attrs, slog.Time(FieldDetectedAt, row.DetectedAt.UTC()))
	}

	if strings.TrimSpace(row.FailureReason) != "" {
		attrs = append(attrs, slog.String(FieldFailureReason, row.FailureReason))
	}

	attrs = appendLatencyClassificationLogAttr(attrs, classification)

	return attrs
}

type TelemetryProcessor struct {
	telemetry *telemetry.Repository
	logger    *slog.Logger
	config    Config
}

func newTelemetryProcessor(telemetryRepo *telemetry.Repository, logger *slog.Logger, config *Config) *TelemetryProcessor {
	return &TelemetryProcessor{
		telemetry: telemetryRepo,
		logger:    logger,
		config:    *config,
	}
}

func (tp *TelemetryProcessor) telemetryLoop(ctx context.Context) {
	tp.processDeliveryTelemetry(ctx)

	if err := lifecycle.RunTickerLoop(ctx, tp.config.TelemetryPollInterval, func(ctx context.Context) error {
		tp.processDeliveryTelemetry(ctx)

		return nil
	}); err != nil {
		logTickerStop(tp.logger, "Delivery telemetry loop stopped with error", err)
	}
}

func (tp *TelemetryProcessor) cleanup(ctx context.Context) {
	if tp == nil || tp.telemetry == nil || tp.config.TelemetryRetention <= 0 {
		return
	}

	deleted, err := tp.telemetry.DeleteLoggedBefore(ctx, time.Now().UTC().Add(-tp.config.TelemetryRetention))
	if err != nil {
		tp.logger.Warn("Failed to cleanup old delivery telemetry", slog.Any("error", err))

		return
	}

	if deleted > 0 {
		tp.logger.Info("Cleaned up old delivery telemetry",
			slog.Int64("deleted", deleted),
			slog.Duration("retention", tp.config.TelemetryRetention))
	}
}

// processDeliveryTelemetry는 lifecycle 전이 트랜잭션이 기록한 시도 행을 로그로 내보낸다.
func (tp *TelemetryProcessor) processDeliveryTelemetry(ctx context.Context) {
	if tp == nil || tp.telemetry == nil {
		return
	}

	rows, ok := tp.fetchDeliveryTelemetryRows(ctx)
	if !ok || len(rows) == 0 {
		return
	}

	classificationsByOutboxID := tp.loadDeliveryTelemetryClassificationsForRows(ctx, rows)
	loggedIDs, failedIDs := tp.emitDeliveryTelemetryRows(rows, classificationsByOutboxID)
	tp.markDeliveryTelemetryResults(ctx, loggedIDs, failedIDs)
}

func (tp *TelemetryProcessor) fetchDeliveryTelemetryRows(ctx context.Context) ([]domain.YouTubeNotificationDeliveryTelemetry, bool) {
	rows, err := tp.telemetry.FetchAndLockPending(ctx, tp.config.TelemetryFlushBatch, tp.config.LockTimeout)
	if err != nil {
		tp.logger.Warn("Failed to fetch delivery telemetry buffer", slog.Any("error", err))

		return nil, false
	}

	return rows, true
}

func (tp *TelemetryProcessor) loadDeliveryTelemetryClassificationsForRows(
	ctx context.Context,
	rows []domain.YouTubeNotificationDeliveryTelemetry,
) map[int64]timeline.PostLatencyClassificationResult {
	classificationsByOutboxID, err := tp.loadDeliveryTelemetryLatencyClassifications(ctx, rows)
	if err != nil {
		tp.logger.Warn("Failed to load delivery telemetry latency classifications",
			slog.Int("rows", len(rows)),
			slog.Any("error", err))
	}

	return classificationsByOutboxID
}

func (tp *TelemetryProcessor) emitDeliveryTelemetryRows(
	rows []domain.YouTubeNotificationDeliveryTelemetry,
	classificationsByOutboxID map[int64]timeline.PostLatencyClassificationResult,
) (loggedIDs, failedIDs []int64) {
	loggedIDs = make([]int64, 0, len(rows))
	failedIDs = make([]int64, 0)

	for i := range rows {
		classification := classificationsByOutboxID[rows[i].OutboxID]
		if err := tp.emitDeliveryTelemetry(&rows[i], &classification); err != nil {
			failedIDs = append(failedIDs, rows[i].ID)
			continue
		}

		loggedIDs = append(loggedIDs, rows[i].ID)
	}

	return loggedIDs, failedIDs
}

func (tp *TelemetryProcessor) markDeliveryTelemetryResults(ctx context.Context, loggedIDs, failedIDs []int64) {
	if err := tp.telemetry.MarkLoggedBatch(ctx, loggedIDs); err != nil {
		tp.logger.Warn("Failed to mark delivery telemetry as logged", slog.Any("error", err))
	}

	if err := tp.telemetry.MarkRetryBatch(ctx, failedIDs, tp.config.TelemetryRetryBackoff, "emit delivery telemetry"); err != nil {
		tp.logger.Warn("Failed to schedule delivery telemetry retry", slog.Any("error", err))
	}
}

func (tp *TelemetryProcessor) loadDeliveryTelemetryLatencyClassifications(
	ctx context.Context,
	rows []domain.YouTubeNotificationDeliveryTelemetry,
) (map[int64]timeline.PostLatencyClassificationResult, error) {
	if tp == nil || tp.telemetry == nil || len(rows) == 0 {
		return map[int64]timeline.PostLatencyClassificationResult{}, nil
	}

	timelines, err := tp.telemetry.ListPostDeliveryTimelinesByOutboxIDs(ctx, telemetry.CollectTelemetryOutboxIDs(rows))
	if err != nil {
		return nil, fmt.Errorf("list post delivery timelines by outbox ids: %w", err)
	}

	classificationsByOutboxID := make(map[int64]timeline.PostLatencyClassificationResult, len(timelines))
	for i := range timelines {
		if timelines[i].OutboxID <= 0 {
			continue
		}

		classificationsByOutboxID[timelines[i].OutboxID] = timelines[i].LatencyClassification
	}

	return classificationsByOutboxID, nil
}

func (tp *TelemetryProcessor) emitDeliveryTelemetry(
	row *domain.YouTubeNotificationDeliveryTelemetry,
	classification *timeline.PostLatencyClassificationResult,
) error {
	if strings.TrimSpace(row.RoomID) == "" {
		return errors.New("delivery telemetry room id is empty")
	}

	attrs := buildDeliveryAuditLogAttrsWithClassification(row, classification)

	attrs = append(attrs, slog.String(FieldTelemetrySource, TelemetrySourcePersistentBuffer))
	tp.logger.Info(CommunityShortsDeliveryAuditMessage, attrs...)

	return nil
}
