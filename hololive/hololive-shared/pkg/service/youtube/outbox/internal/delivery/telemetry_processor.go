package delivery

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/logschema"
	"github.com/park285/shared-go/pkg/runtime/loop"
)

func buildDeliveryAuditLogAttrsWithClassification(row domain.YouTubeNotificationDeliveryTelemetry, classification PostLatencyClassificationResult) []any {
	attrs := []any{
		slog.Int64(logschema.FieldDeliveryID, row.DeliveryID),
		slog.Int64(logschema.FieldOutboxID, row.OutboxID),
		slog.String(logschema.FieldRoomID, row.RoomID),
		slog.String(logschema.FieldChannelID, row.ChannelID),
		slog.String(deliveryAuditPostIDLogField, row.PostID),
		slog.String(deliveryAuditContentIDLogField, strings.TrimSpace(row.ContentID)),
		slog.String(deliveryAuditAlarmTypeLogField, string(row.AlarmType)),
		slog.Time(deliveryAuditSentAtLogField, row.EventAt.UTC()),
		slog.String(deliveryAuditSendResultLogField, row.SendResult),
		slog.String(deliveryAuditPathLogField, normalizeCommunityShortsDeliveryPath(row.DeliveryPath)),
		slog.String(deliveryAuditModeLogField, row.DeliveryMode),
		slog.String(deliveryDedupeKeyLogField, row.DedupeKey),
		slog.Int(logschema.FieldAttemptOrdinal, row.AttemptOrdinal),
	}
	attrs = appendCommunityShortsAlarmTimingLogAttrs(attrs, communityShortsAlarmTimingForTelemetryRow(row))
	attrs = appendDeliveryObservationLogAttrs(attrs, row)
	if strings.TrimSpace(row.FailureReason) != "" {
		attrs = append(attrs, slog.String(deliveryAuditFailureReasonLogField, row.FailureReason))
	}
	attrs = appendLatencyClassificationLogAttr(attrs, classification)
	return attrs
}

func appendDeliveryObservationLogAttrs(attrs []any, row domain.YouTubeNotificationDeliveryTelemetry) []any {
	if row.DetectedAt != nil {
		attrs = append(attrs, slog.Time(logschema.FieldDetectedAt, row.DetectedAt.UTC()))
	}
	attrs = append(attrs, slog.String(logschema.FieldObservationStatus, normalizeDeliveryTelemetryObservationStatus(row.ObservationStatus)))
	if strings.TrimSpace(row.ObservationRuntimeName) != "" {
		attrs = append(attrs, slog.String(logschema.FieldObservationRuntimeName, strings.TrimSpace(row.ObservationRuntimeName)))
	}
	if row.ObservationBigBangCutoverAt != nil {
		attrs = append(attrs, slog.Time(logschema.FieldObservationBigBangCutoverAt, row.ObservationBigBangCutoverAt.UTC()))
	}
	if row.ObservationStartedAt != nil {
		attrs = append(attrs, slog.Time(logschema.FieldObservationStartedAt, row.ObservationStartedAt.UTC()))
	}
	if row.ObservationEndedAt != nil {
		attrs = append(attrs, slog.Time(logschema.FieldObservationEndedAt, row.ObservationEndedAt.UTC()))
	}
	return attrs
}

type TelemetryProcessor struct {
	telemetry *DeliveryTelemetryRepository
	logger    *slog.Logger
	config    Config
}

func newTelemetryProcessor(telemetry *DeliveryTelemetryRepository, logger *slog.Logger, config Config) *TelemetryProcessor {
	return &TelemetryProcessor{
		telemetry: telemetry,
		logger:    logger,
		config:    config,
	}
}

func (tp *TelemetryProcessor) telemetryLoop(ctx context.Context) {
	tp.processDeliveryTelemetry(ctx)
	_ = loop.RunTickerLoop(ctx, tp.config.TelemetryPollInterval, func(ctx context.Context) error {
		tp.processDeliveryTelemetry(ctx)
		return nil
	})
}

func (tp *TelemetryProcessor) processDeliveryTelemetry(ctx context.Context) {
	if tp == nil || tp.telemetry == nil {
		return
	}

	tp.backfillDeliveryTelemetry(ctx)
	rows, ok := tp.fetchDeliveryTelemetryRows(ctx)
	if !ok || len(rows) == 0 {
		return
	}

	classificationsByOutboxID := tp.loadDeliveryTelemetryClassificationsForRows(ctx, rows)
	loggedIDs, failedIDs := tp.emitDeliveryTelemetryRows(rows, classificationsByOutboxID)
	tp.markDeliveryTelemetryResults(ctx, loggedIDs, failedIDs)
}

func (tp *TelemetryProcessor) backfillDeliveryTelemetry(ctx context.Context) {
	if _, err := tp.telemetry.BackfillFromDelivery(ctx, tp.config.TelemetryBackfillBatch, tp.deliveryTelemetryBackfillSince()); err != nil {
		tp.logger.Warn("Failed to backfill delivery telemetry", slog.Any("error", err))
	}
}

func (tp *TelemetryProcessor) deliveryTelemetryBackfillSince() time.Time {
	if tp.config.TelemetryRetention <= 0 {
		return time.Time{}
	}
	return time.Now().UTC().Add(-tp.config.TelemetryRetention)
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
) map[int64]PostLatencyClassificationResult {
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
	classificationsByOutboxID map[int64]PostLatencyClassificationResult,
) ([]int64, []int64) {
	loggedIDs := make([]int64, 0, len(rows))
	failedIDs := make([]int64, 0)
	for i := range rows {
		if err := tp.emitDeliveryTelemetry(rows[i], classificationsByOutboxID[rows[i].OutboxID]); err != nil {
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
) (map[int64]PostLatencyClassificationResult, error) {
	if tp == nil || tp.telemetry == nil || len(rows) == 0 {
		return nil, nil
	}

	timelines, err := tp.telemetry.ListPostDeliveryTimelinesByOutboxIDs(ctx, collectTelemetryOutboxIDs(rows))
	if err != nil {
		return nil, err
	}

	classificationsByOutboxID := make(map[int64]PostLatencyClassificationResult, len(timelines))
	for i := range timelines {
		if timelines[i].OutboxID <= 0 {
			continue
		}
		classificationsByOutboxID[timelines[i].OutboxID] = timelines[i].LatencyClassification
	}
	return classificationsByOutboxID, nil
}

func (tp *TelemetryProcessor) emitDeliveryTelemetry(
	row domain.YouTubeNotificationDeliveryTelemetry,
	classification PostLatencyClassificationResult,
) error {
	if strings.TrimSpace(row.RoomID) == "" {
		return fmt.Errorf("delivery telemetry room id is empty")
	}
	applyTelemetryPostID(&row)

	attrs := buildDeliveryAuditLogAttrsWithClassification(row, classification)
	attrs = append(attrs, slog.String(logschema.FieldTelemetrySource, logschema.TelemetrySourcePersistentBuffer))
	tp.logger.Info(deliveryAuditLogMessage, attrs...)
	return nil
}
