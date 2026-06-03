package delivery

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/kapu/hololive-shared/internal/dbx"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/internal/delivery/deliverysql"
)

type DeliveryTelemetryRepository struct {
	db dbx.Querier
}

func NewDeliveryTelemetryRepository(db any) *DeliveryTelemetryRepository {
	return &DeliveryTelemetryRepository{db: asQuerier(db)}
}

func asQuerier(db any) dbx.Querier {
	if deliverysql.IsNilDB(db) {
		return nil
	}
	if typed, ok := db.(dbx.Querier); ok {
		return typed
	}
	return nil
}

func cloneUTCTimePtr(value *time.Time) *time.Time {
	if value == nil || value.IsZero() {
		return nil
	}

	normalized := value.UTC()
	return &normalized
}

func (r *DeliveryTelemetryRepository) Enqueue(ctx context.Context, rows []domain.YouTubeNotificationDeliveryTelemetry) error {
	prepared, err := r.prepareRows(ctx, rows)
	if err != nil {
		return err
	}
	return r.enqueuePrepared(ctx, prepared)
}

func (r *DeliveryTelemetryRepository) prepareRows(
	ctx context.Context,
	rows []domain.YouTubeNotificationDeliveryTelemetry,
) ([]domain.YouTubeNotificationDeliveryTelemetry, error) {
	if len(rows) == 0 {
		return nil, nil
	}

	normalized := make([]domain.YouTubeNotificationDeliveryTelemetry, 0, len(rows))
	now := time.Now().UTC()
	for i := range rows {
		if row, ok := prepareDeliveryTelemetryRow(rows[i], now); ok {
			normalized = append(normalized, row)
		}
	}
	if len(normalized) == 0 {
		return nil, nil
	}
	if err := r.enrichRows(ctx, normalized); err != nil {
		return nil, err
	}

	return normalized, nil
}

func prepareDeliveryTelemetryRow(
	row domain.YouTubeNotificationDeliveryTelemetry,
	now time.Time,
) (domain.YouTubeNotificationDeliveryTelemetry, bool) {
	if row.DeliveryID <= 0 || row.AttemptOrdinal <= 0 {
		return domain.YouTubeNotificationDeliveryTelemetry{}, false
	}

	normalizeDeliveryTelemetryAttemptTimes(&row, now)
	applyDeliveryTelemetryTiming(&row)
	applyDeliveryTelemetryDefaults(&row, now)
	return row, true
}

func normalizeDeliveryTelemetryAttemptTimes(row *domain.YouTubeNotificationDeliveryTelemetry, now time.Time) {
	row.AttemptStartedAt = cloneUTCTimePtr(row.AttemptStartedAt)
	row.AttemptFinishedAt = cloneUTCTimePtr(row.AttemptFinishedAt)
	if row.AttemptFinishedAt == nil && !row.EventAt.IsZero() {
		finishedAt := row.EventAt.UTC()
		row.AttemptFinishedAt = &finishedAt
	}
	if row.EventAt.IsZero() && row.AttemptFinishedAt != nil {
		row.EventAt = row.AttemptFinishedAt.UTC()
	}
	if row.EventAt.IsZero() {
		row.EventAt = now
	}
	if row.AttemptFinishedAt == nil {
		finishedAt := row.EventAt.UTC()
		row.AttemptFinishedAt = &finishedAt
	}
}

func applyDeliveryTelemetryTiming(row *domain.YouTubeNotificationDeliveryTelemetry) {
	timing := communityShortsAlarmTimingForTelemetryRow(*row)
	row.ActualPublishedAt = timing.ActualPublishedAt
	row.AlarmSentAt = timing.AlarmSentAt
	row.AlarmLatencyMillis = clonePostLatencyInt64(timing.AlarmLatencyMillis)
}

func applyDeliveryTelemetryDefaults(row *domain.YouTubeNotificationDeliveryTelemetry, now time.Time) {
	if row.NextAttemptAt.IsZero() {
		row.NextAttemptAt = now
	}
	row.DeliveryPath = normalizeCommunityShortsDeliveryPath(row.DeliveryPath)
	applyTelemetryPostID(row)
	row.ObservationStatus = normalizeDeliveryTelemetryObservationStatus(row.ObservationStatus)
}

func (r *DeliveryTelemetryRepository) enqueuePrepared(
	ctx context.Context,
	rows []domain.YouTubeNotificationDeliveryTelemetry,
) error {
	if len(rows) == 0 {
		return nil
	}
	if r == nil || r.db == nil {
		return fmt.Errorf("enqueue delivery telemetry: db is nil")
	}

	for i := range rows {
		if _, err := r.db.Exec(ctx, `
			INSERT INTO youtube_notification_delivery_telemetry (
				delivery_id, attempt_ordinal, outbox_id, channel_id, content_id, post_id, room_id, alarm_type,
				actual_published_at, alarm_sent_at, alarm_latency_millis, detected_at,
				observation_status, observation_runtime_name, observation_bigbang_cutover_at, observation_started_at, observation_ended_at,
				dedupe_key, delivery_path, delivery_mode, send_result, failure_reason,
				attempt_started_at, attempt_finished_at, event_at, next_attempt_at, locked_at, logged_at, error
			) VALUES (
				$1, $2, $3, $4, $5, $6, $7, $8,
				$9, $10, $11, $12,
				$13, $14, $15, $16, $17,
				$18, $19, $20, $21, $22,
				$23, $24, $25, $26, $27, $28, $29
			)
			ON CONFLICT (delivery_id, attempt_ordinal) DO NOTHING
		`, rows[i].DeliveryID, rows[i].AttemptOrdinal, rows[i].OutboxID, rows[i].ChannelID, rows[i].ContentID, rows[i].PostID, rows[i].RoomID, rows[i].AlarmType,
			rows[i].ActualPublishedAt, rows[i].AlarmSentAt, rows[i].AlarmLatencyMillis, rows[i].DetectedAt,
			rows[i].ObservationStatus, rows[i].ObservationRuntimeName, rows[i].ObservationBigBangCutoverAt, rows[i].ObservationStartedAt, rows[i].ObservationEndedAt,
			rows[i].DedupeKey, rows[i].DeliveryPath, rows[i].DeliveryMode, rows[i].SendResult, rows[i].FailureReason,
			rows[i].AttemptStartedAt, rows[i].AttemptFinishedAt, rows[i].EventAt, rows[i].NextAttemptAt, rows[i].LockedAt, rows[i].LoggedAt, rows[i].Error); err != nil {
			return fmt.Errorf("enqueue delivery telemetry: %w", err)
		}
	}

	return nil
}

func deliveryTelemetrySelectColumns() string {
	return `id, delivery_id, attempt_ordinal, outbox_id, channel_id, content_id, post_id, room_id, alarm_type,
		actual_published_at, alarm_sent_at, alarm_latency_millis, detected_at,
		observation_status, observation_runtime_name, observation_bigbang_cutover_at, observation_started_at, observation_ended_at,
		dedupe_key, delivery_path, delivery_mode, send_result, failure_reason,
		attempt_started_at, attempt_finished_at, event_at, next_attempt_at, created_at, locked_at, logged_at, error`
}

func deliveryTelemetrySelectColumnsWithAlias(alias string) string {
	columns := strings.Split(deliveryTelemetrySelectColumns(), ",")
	for i := range columns {
		columns[i] = alias + "." + strings.TrimSpace(columns[i])
	}
	return strings.Join(columns, ", ")
}

func scanTelemetryRow(row pgx.CollectableRow) (domain.YouTubeNotificationDeliveryTelemetry, error) {
	var item domain.YouTubeNotificationDeliveryTelemetry
	err := row.Scan(
		&item.ID, &item.DeliveryID, &item.AttemptOrdinal, &item.OutboxID, &item.ChannelID, &item.ContentID, &item.PostID, &item.RoomID, &item.AlarmType,
		&item.ActualPublishedAt, &item.AlarmSentAt, &item.AlarmLatencyMillis, &item.DetectedAt,
		&item.ObservationStatus, &item.ObservationRuntimeName, &item.ObservationBigBangCutoverAt, &item.ObservationStartedAt, &item.ObservationEndedAt,
		&item.DedupeKey, &item.DeliveryPath, &item.DeliveryMode, &item.SendResult, &item.FailureReason,
		&item.AttemptStartedAt, &item.AttemptFinishedAt, &item.EventAt, &item.NextAttemptAt, &item.CreatedAt, &item.LockedAt, &item.LoggedAt, &item.Error,
	)
	return item, err
}

func (r *DeliveryTelemetryRepository) queryTelemetryRows(ctx context.Context, action string, query string, args ...any) ([]domain.YouTubeNotificationDeliveryTelemetry, error) {
	rows, err := r.db.Query(ctx, deliverysql.PostgresPlaceholders(query), args...)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", action, err)
	}
	defer rows.Close()
	items, err := pgx.CollectRows(rows, scanTelemetryRow)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", action, err)
	}
	return items, nil
}

func (r *DeliveryTelemetryRepository) FetchAndLockPending(ctx context.Context, batchSize int, lockTimeout time.Duration) ([]domain.YouTubeNotificationDeliveryTelemetry, error) {
	if batchSize <= 0 {
		return nil, nil
	}

	now := time.Now().UTC()
	lockExpiry := now.Add(-lockTimeout)

	candidates, err := r.queryTelemetryRows(ctx, "fetch pending delivery telemetry", `
		SELECT `+deliveryTelemetrySelectColumns()+`
		FROM youtube_notification_delivery_telemetry
		WHERE logged_at IS NULL
		  AND next_attempt_at <= $1
		  AND (locked_at IS NULL OR locked_at < $2)
		ORDER BY event_at ASC
		LIMIT $3
	`, now, lockExpiry, batchSize)
	if err != nil {
		return nil, err
	}
	if len(candidates) == 0 {
		return nil, nil
	}

	candidateIDs := make([]int64, 0, len(candidates))
	for i := range candidates {
		candidateIDs = append(candidateIDs, candidates[i].ID)
	}
	slices.Sort(candidateIDs)

	lockArgs := []any{now}
	lockArgs = deliverysql.AppendDeliveryInt64Args(lockArgs, candidateIDs)
	lockArgs = append(lockArgs, lockExpiry)
	if _, err := deliverysql.ExecDeliverySQL(ctx, r.db, "lock delivery telemetry rows", `
		UPDATE youtube_notification_delivery_telemetry
		SET locked_at = ?
		WHERE `+deliverysql.DeliveryInClause("id", len(candidateIDs))+`
		  AND logged_at IS NULL
		  AND (locked_at IS NULL OR locked_at < ?)
	`, lockArgs...); err != nil {
		return nil, err
	}

	reloadArgs := deliverysql.AppendDeliveryInt64Args(nil, candidateIDs)
	reloadArgs = append(reloadArgs, now)
	locked, err := r.queryTelemetryRows(ctx, "reload locked delivery telemetry rows", `
		SELECT `+deliveryTelemetrySelectColumns()+`
		FROM youtube_notification_delivery_telemetry
		WHERE `+deliverysql.DeliveryInClause("id", len(candidateIDs))+`
		  AND locked_at = ?
		ORDER BY event_at ASC
	`, reloadArgs...)
	if err != nil {
		return nil, err
	}
	if err := r.refreshLockedRows(ctx, locked); err != nil {
		return nil, err
	}

	return locked, nil
}

func (r *DeliveryTelemetryRepository) MarkLoggedBatch(ctx context.Context, ids []int64) error {
	uniqueIDs := deliverysql.UniqueInt64s(ids)
	if len(uniqueIDs) == 0 {
		return nil
	}

	now := time.Now().UTC()
	args := []any{now}
	args = deliverysql.AppendDeliveryInt64Args(args, uniqueIDs)
	if _, err := deliverysql.ExecDeliverySQL(ctx, r.db, "mark delivery telemetry logged", `
		UPDATE youtube_notification_delivery_telemetry
		SET logged_at = ?, locked_at = NULL, error = ''
		WHERE `+deliverysql.DeliveryInClause("id", len(uniqueIDs))+`
	`, args...); err != nil {
		return err
	}

	return nil
}

func (r *DeliveryTelemetryRepository) MarkRetryBatch(ctx context.Context, ids []int64, backoff time.Duration, errMsg string) error {
	uniqueIDs := deliverysql.UniqueInt64s(ids)
	if len(uniqueIDs) == 0 {
		return nil
	}

	nextAttemptAt := time.Now().UTC().Add(backoff)
	args := []any{nextAttemptAt, deliverysql.TruncateString(errMsg, 500)}
	args = deliverysql.AppendDeliveryInt64Args(args, uniqueIDs)
	if _, err := deliverysql.ExecDeliverySQL(ctx, r.db, "mark delivery telemetry retry", `
		UPDATE youtube_notification_delivery_telemetry
		SET locked_at = NULL, next_attempt_at = ?, error = ?
		WHERE `+deliverysql.DeliveryInClause("id", len(uniqueIDs))+`
	`, args...); err != nil {
		return err
	}

	return nil
}

func (r *DeliveryTelemetryRepository) refreshLockedRows(
	ctx context.Context,
	rows []domain.YouTubeNotificationDeliveryTelemetry,
) error {
	if len(rows) == 0 {
		return nil
	}

	enriched := append([]domain.YouTubeNotificationDeliveryTelemetry(nil), rows...)
	if err := r.enrichRows(ctx, enriched); err != nil {
		return fmt.Errorf("refresh locked delivery telemetry rows: %w", err)
	}

	for i := range enriched {
		if !deliveryTelemetryObservationContextChanged(rows[i], enriched[i]) {
			rows[i] = enriched[i]
			continue
		}

		if _, err := r.db.Exec(ctx, `
			UPDATE youtube_notification_delivery_telemetry
			SET actual_published_at = $1,
			    alarm_sent_at = $2,
			    alarm_latency_millis = $3,
			    detected_at = $4,
			    observation_status = $5,
			    observation_runtime_name = $6,
			    observation_bigbang_cutover_at = $7,
			    observation_started_at = $8,
			    observation_ended_at = $9
			WHERE id = $10
		`, enriched[i].ActualPublishedAt, enriched[i].AlarmSentAt, enriched[i].AlarmLatencyMillis, enriched[i].DetectedAt,
			normalizeDeliveryTelemetryObservationStatus(enriched[i].ObservationStatus), strings.TrimSpace(enriched[i].ObservationRuntimeName),
			enriched[i].ObservationBigBangCutoverAt, enriched[i].ObservationStartedAt, enriched[i].ObservationEndedAt, enriched[i].ID); err != nil {
			return fmt.Errorf("refresh locked delivery telemetry rows: update row %d: %w", enriched[i].ID, err)
		}
		rows[i] = enriched[i]
	}

	return nil
}

func (r *DeliveryTelemetryRepository) DeleteLoggedBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	if r == nil || r.db == nil || cutoff.IsZero() {
		return 0, nil
	}

	tag, err := r.db.Exec(ctx, `
		DELETE FROM youtube_notification_delivery_telemetry
		WHERE logged_at IS NOT NULL AND event_at < $1
	`, cutoff.UTC())
	if err != nil {
		return 0, fmt.Errorf("delete delivery telemetry before cutoff: %w", err)
	}
	return tag.RowsAffected(), nil
}

func collectTelemetryOutboxIDs(rows []domain.YouTubeNotificationDeliveryTelemetry) []int64 {
	outboxIDs := make([]int64, 0, len(rows))
	for i := range rows {
		if rows[i].OutboxID <= 0 {
			continue
		}
		outboxIDs = append(outboxIDs, rows[i].OutboxID)
	}
	return deliverysql.UniqueInt64s(outboxIDs)
}
