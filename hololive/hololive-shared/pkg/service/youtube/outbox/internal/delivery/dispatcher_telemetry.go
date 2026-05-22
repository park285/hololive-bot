package delivery

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/logschema"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/runtime/loop"
)

func (d *Dispatcher) telemetryLoop(ctx context.Context) {
	d.processDeliveryTelemetry(ctx)
	_ = loop.RunTickerLoop(ctx, d.cfg.TelemetryPollInterval, func(ctx context.Context) error {
		d.processDeliveryTelemetry(ctx)
		return nil
	})
}

func (d *Dispatcher) processDeliveryTelemetry(ctx context.Context) {
	if d == nil || d.telemetry == nil {
		return
	}

	d.backfillDeliveryTelemetry(ctx)
	rows, ok := d.fetchDeliveryTelemetryRows(ctx)
	if !ok || len(rows) == 0 {
		return
	}

	classificationsByOutboxID := d.loadDeliveryTelemetryClassificationsForRows(ctx, rows)
	loggedIDs, failedIDs := d.emitDeliveryTelemetryRows(rows, classificationsByOutboxID)
	d.markDeliveryTelemetryResults(ctx, loggedIDs, failedIDs)
}

func (d *Dispatcher) backfillDeliveryTelemetry(ctx context.Context) {
	if _, err := d.telemetry.BackfillFromDelivery(ctx, d.cfg.TelemetryBackfillBatch, d.deliveryTelemetryBackfillSince()); err != nil {
		d.logger.Warn("Failed to backfill delivery telemetry", slog.Any("error", err))
	}
}

func (d *Dispatcher) deliveryTelemetryBackfillSince() time.Time {
	if d.cfg.TelemetryRetention <= 0 {
		return time.Time{}
	}
	return time.Now().UTC().Add(-d.cfg.TelemetryRetention)
}

func (d *Dispatcher) fetchDeliveryTelemetryRows(ctx context.Context) ([]domain.YouTubeNotificationDeliveryTelemetry, bool) {
	rows, err := d.telemetry.FetchAndLockPending(ctx, d.cfg.TelemetryFlushBatch, d.cfg.LockTimeout)
	if err != nil {
		d.logger.Warn("Failed to fetch delivery telemetry buffer", slog.Any("error", err))
		return nil, false
	}
	return rows, true
}

func (d *Dispatcher) loadDeliveryTelemetryClassificationsForRows(
	ctx context.Context,
	rows []domain.YouTubeNotificationDeliveryTelemetry,
) map[int64]PostLatencyClassificationResult {
	classificationsByOutboxID, err := d.loadDeliveryTelemetryLatencyClassifications(ctx, rows)
	if err != nil {
		d.logger.Warn("Failed to load delivery telemetry latency classifications",
			slog.Int("rows", len(rows)),
			slog.Any("error", err))
	}
	return classificationsByOutboxID
}

func (d *Dispatcher) emitDeliveryTelemetryRows(
	rows []domain.YouTubeNotificationDeliveryTelemetry,
	classificationsByOutboxID map[int64]PostLatencyClassificationResult,
) ([]int64, []int64) {
	loggedIDs := make([]int64, 0, len(rows))
	failedIDs := make([]int64, 0)
	for i := range rows {
		if err := d.emitDeliveryTelemetry(rows[i], classificationsByOutboxID[rows[i].OutboxID]); err != nil {
			failedIDs = append(failedIDs, rows[i].ID)
			continue
		}
		loggedIDs = append(loggedIDs, rows[i].ID)
	}
	return loggedIDs, failedIDs
}

func (d *Dispatcher) markDeliveryTelemetryResults(ctx context.Context, loggedIDs, failedIDs []int64) {
	if err := d.telemetry.MarkLoggedBatch(ctx, loggedIDs); err != nil {
		d.logger.Warn("Failed to mark delivery telemetry as logged", slog.Any("error", err))
	}
	if err := d.telemetry.MarkRetryBatch(ctx, failedIDs, d.cfg.TelemetryRetryBackoff, "emit delivery telemetry"); err != nil {
		d.logger.Warn("Failed to schedule delivery telemetry retry", slog.Any("error", err))
	}
}

func (d *Dispatcher) loadDeliveryTelemetryLatencyClassifications(
	ctx context.Context,
	rows []domain.YouTubeNotificationDeliveryTelemetry,
) (map[int64]PostLatencyClassificationResult, error) {
	if d == nil || d.telemetry == nil || len(rows) == 0 {
		return nil, nil
	}

	timelines, err := d.telemetry.ListPostDeliveryTimelinesByOutboxIDs(ctx, collectTelemetryOutboxIDs(rows))
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

func (d *Dispatcher) emitDeliveryTelemetry(
	row domain.YouTubeNotificationDeliveryTelemetry,
	classification PostLatencyClassificationResult,
) error {
	if strings.TrimSpace(row.RoomID) == "" {
		return fmt.Errorf("delivery telemetry room id is empty")
	}
	applyTelemetryPostID(&row)

	attrs := buildDeliveryAuditLogAttrsWithClassification(row, classification)
	attrs = append(attrs, slog.String(logschema.FieldTelemetrySource, logschema.TelemetrySourcePersistentBuffer))
	d.logger.Info(deliveryAuditLogMessage, attrs...)
	return nil
}

func buildDeliveryAuditLogAttrs(row domain.YouTubeNotificationDeliveryTelemetry) []any {
	return buildDeliveryAuditLogAttrsWithClassification(row, PostLatencyClassificationResult{})
}

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
