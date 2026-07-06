package telemetry

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/kapu/hololive-shared/internal/dbx"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/internal/delivery/deliverysql"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/internal/delivery/timeline"
)

type Repository struct {
	db dbx.Querier
}

func NewRepository(db any) *Repository {
	return &Repository{db: deliverysql.AsQuerier(db)}
}

func (r *Repository) Enqueue(ctx context.Context, rows []domain.YouTubeNotificationDeliveryTelemetry) error {
	prepared, err := r.PrepareRows(ctx, rows)
	if err != nil {
		return err
	}
	return r.EnqueuePrepared(ctx, prepared)
}

func (r *Repository) PrepareRows(
	ctx context.Context,
	rows []domain.YouTubeNotificationDeliveryTelemetry,
) ([]domain.YouTubeNotificationDeliveryTelemetry, error) {
	if len(rows) == 0 {
		return nil, nil
	}

	normalized := make([]domain.YouTubeNotificationDeliveryTelemetry, 0, len(rows))
	now := time.Now().UTC()
	for i := range rows {
		if row, ok := prepareDeliveryTelemetryRow(&rows[i], now); ok {
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
	row *domain.YouTubeNotificationDeliveryTelemetry,
	now time.Time,
) (domain.YouTubeNotificationDeliveryTelemetry, bool) {
	if row.DeliveryID <= 0 || row.AttemptOrdinal <= 0 {
		return domain.YouTubeNotificationDeliveryTelemetry{}, false
	}

	normalizeDeliveryTelemetryAttemptTimes(row, now)
	applyDeliveryTelemetryTiming(row)
	applyDeliveryTelemetryDefaults(row, now)
	return *row, true
}

func normalizeDeliveryTelemetryAttemptTimes(row *domain.YouTubeNotificationDeliveryTelemetry, now time.Time) {
	row.AttemptStartedAt = deliverysql.CloneUTCTimePtr(row.AttemptStartedAt)
	row.AttemptFinishedAt = deliverysql.CloneUTCTimePtr(row.AttemptFinishedAt)
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
	timing := communityShortsAlarmTimingForTelemetryRow(row)
	row.ActualPublishedAt = timing.ActualPublishedAt
	row.AlarmSentAt = timing.AlarmSentAt
	row.AlarmLatencyMillis = timeline.ClonePostLatencyInt64(timing.AlarmLatencyMillis)
}

func applyDeliveryTelemetryDefaults(row *domain.YouTubeNotificationDeliveryTelemetry, now time.Time) {
	if row.NextAttemptAt.IsZero() {
		row.NextAttemptAt = now
	}
	row.DeliveryPath = NormalizeCommunityShortsDeliveryPath(row.DeliveryPath)
	ApplyTelemetryPostID(row)
}

const (
	enqueueTelemetryChunkSize     = 500
	enqueueTelemetryColumnsPerRow = 24
)

func (r *Repository) EnqueuePrepared(
	ctx context.Context,
	rows []domain.YouTubeNotificationDeliveryTelemetry,
) error {
	if len(rows) == 0 {
		return nil
	}
	if r == nil || r.db == nil {
		return fmt.Errorf("enqueue delivery telemetry: db is nil")
	}

	for start := 0; start < len(rows); start += enqueueTelemetryChunkSize {
		end := min(start+enqueueTelemetryChunkSize, len(rows))
		if err := r.enqueuePreparedChunk(ctx, rows[start:end]); err != nil {
			return err
		}
	}

	return nil
}

func (r *Repository) enqueuePreparedChunk(ctx context.Context, rows []domain.YouTubeNotificationDeliveryTelemetry) error {
	var sb strings.Builder
	sb.WriteString(mustSQL("repository_0136_01.sql"))

	args := make([]any, 0, len(rows)*enqueueTelemetryColumnsPerRow)
	for i := range rows {
		if i > 0 {
			sb.WriteByte(',')
		}
		writeTelemetryRowPlaceholders(&sb, i*enqueueTelemetryColumnsPerRow)
		args = appendTelemetryRowArgs(args, &rows[i])
	}
	sb.WriteString(mustSQL("repository_0152_02.sql"))

	if _, err := r.db.Exec(ctx, sb.String(), args...); err != nil {
		return fmt.Errorf("enqueue delivery telemetry: %w", err)
	}

	return nil
}

func writeTelemetryRowPlaceholders(sb *strings.Builder, base int) {
	sb.WriteByte('(')
	for j := range enqueueTelemetryColumnsPerRow {
		if j > 0 {
			sb.WriteByte(',')
		}
		sb.WriteByte('$')
		sb.WriteString(strconv.Itoa(base + j + 1))
	}
	sb.WriteByte(')')
}

func appendTelemetryRowArgs(args []any, row *domain.YouTubeNotificationDeliveryTelemetry) []any {
	return append(args, row.DeliveryID, row.AttemptOrdinal, row.OutboxID, row.ChannelID, row.ContentID, row.PostID, row.RoomID, row.AlarmType,
		row.ActualPublishedAt, row.AlarmSentAt, row.AlarmLatencyMillis, row.DetectedAt,
		row.DedupeKey, row.DeliveryPath, row.DeliveryMode, row.SendResult, row.FailureReason,
		row.AttemptStartedAt, row.AttemptFinishedAt, row.EventAt, row.NextAttemptAt, row.LockedAt, row.LoggedAt, row.Error)
}

func deliveryTelemetrySelectColumns() string {
	return `id, delivery_id, attempt_ordinal, outbox_id, channel_id, content_id, post_id, room_id, alarm_type,
		actual_published_at, alarm_sent_at, alarm_latency_millis, detected_at,
		dedupe_key, delivery_path, delivery_mode, send_result, failure_reason,
		attempt_started_at, attempt_finished_at, event_at, next_attempt_at, created_at, locked_at, logged_at, error`
}

func (r *Repository) queryTelemetryRows(ctx context.Context, action, query string, args ...any) ([]domain.YouTubeNotificationDeliveryTelemetry, error) {
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

func (r *Repository) FetchAndLockPending(ctx context.Context, batchSize int, lockTimeout time.Duration) ([]domain.YouTubeNotificationDeliveryTelemetry, error) {
	if batchSize <= 0 {
		return nil, nil
	}

	now := time.Now().UTC()
	lockExpiry := now.Add(-lockTimeout)

	candidates, err := r.queryTelemetryRows(ctx, "fetch pending delivery telemetry", mustSQL("repository_0208_03.sql")+deliveryTelemetrySelectColumns()+`
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

	err = r.lockPendingTelemetryRows(ctx, candidateIDs, now, lockExpiry)
	if err != nil {
		return nil, err
	}

	reloadArgs := deliverysql.AppendDeliveryInt64Args(nil, candidateIDs)
	reloadArgs = append(reloadArgs, now)
	locked, err := r.queryTelemetryRows(ctx, "reload locked delivery telemetry rows", mustSQL("repository_0237_04.sql")+deliveryTelemetrySelectColumns()+`
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

func (r *Repository) lockPendingTelemetryRows(ctx context.Context, candidateIDs []int64, now, lockExpiry time.Time) error {
	lockArgs := []any{now}
	lockArgs = deliverysql.AppendDeliveryInt64Args(lockArgs, candidateIDs)
	lockArgs = append(lockArgs, lockExpiry)
	if _, err := deliverysql.ExecDeliverySQL(ctx, r.db, "lock delivery telemetry rows", mustSQL("repository_0258_05.sql")+deliverysql.DeliveryInClause("id", len(candidateIDs))+`
		  AND logged_at IS NULL
		  AND (locked_at IS NULL OR locked_at < ?)
	`, lockArgs...); err != nil {
		return fmt.Errorf("lock delivery telemetry rows: %w", err)
	}
	return nil
}

func (r *Repository) MarkLoggedBatch(ctx context.Context, ids []int64) error {
	uniqueIDs := deliverysql.UniqueInt64s(ids)
	if len(uniqueIDs) == 0 {
		return nil
	}

	now := time.Now().UTC()
	args := []any{now}
	args = deliverysql.AppendDeliveryInt64Args(args, uniqueIDs)
	if _, err := deliverysql.ExecDeliverySQL(ctx, r.db, "mark delivery telemetry logged", mustSQL("repository_0279_06.sql")+deliverysql.DeliveryInClause("id", len(uniqueIDs))+`
	`, args...); err != nil {
		return fmt.Errorf("mark delivery telemetry logged: %w", err)
	}

	return nil
}

func (r *Repository) MarkRetryBatch(ctx context.Context, ids []int64, backoff time.Duration, errMsg string) error {
	uniqueIDs := deliverysql.UniqueInt64s(ids)
	if len(uniqueIDs) == 0 {
		return nil
	}

	nextAttemptAt := time.Now().UTC().Add(backoff)
	args := []any{nextAttemptAt, deliverysql.TruncateString(errMsg, 500)}
	args = deliverysql.AppendDeliveryInt64Args(args, uniqueIDs)
	if _, err := deliverysql.ExecDeliverySQL(ctx, r.db, "mark delivery telemetry retry", mustSQL("repository_0299_07.sql")+deliverysql.DeliveryInClause("id", len(uniqueIDs))+`
	`, args...); err != nil {
		return fmt.Errorf("mark delivery telemetry retry: %w", err)
	}

	return nil
}

func (r *Repository) refreshLockedRows(
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

	ids := make([]int64, 0, len(enriched))
	actualPublishedAt := make([]*time.Time, 0, len(enriched))
	alarmSentAt := make([]*time.Time, 0, len(enriched))
	alarmLatencyMillis := make([]*int64, 0, len(enriched))
	detectedAt := make([]*time.Time, 0, len(enriched))
	for i := range enriched {
		if deliveryTelemetryTrackingContextChanged(&rows[i], &enriched[i]) {
			ids = append(ids, enriched[i].ID)
			actualPublishedAt = append(actualPublishedAt, enriched[i].ActualPublishedAt)
			alarmSentAt = append(alarmSentAt, enriched[i].AlarmSentAt)
			alarmLatencyMillis = append(alarmLatencyMillis, enriched[i].AlarmLatencyMillis)
			detectedAt = append(detectedAt, enriched[i].DetectedAt)
		}
		rows[i] = enriched[i]
	}

	if len(ids) == 0 {
		return nil
	}

	if _, err := deliverysql.ExecDeliverySQL(ctx, r.db, "refresh locked delivery telemetry rows", mustSQL("repository_0343_08.sql"), ids, actualPublishedAt, alarmSentAt, alarmLatencyMillis, detectedAt); err != nil {
		return err
	}

	return nil
}

func (r *Repository) DeleteLoggedBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	if r == nil || r.db == nil || cutoff.IsZero() {
		return 0, nil
	}

	tag, err := r.db.Exec(ctx, mustSQL("repository_0364_09.sql"), cutoff.UTC())
	if err != nil {
		return 0, fmt.Errorf("delete delivery telemetry before cutoff: %w", err)
	}
	return tag.RowsAffected(), nil
}

func CollectTelemetryOutboxIDs(rows []domain.YouTubeNotificationDeliveryTelemetry) []int64 {
	outboxIDs := make([]int64, 0, len(rows))
	for i := range rows {
		if rows[i].OutboxID <= 0 {
			continue
		}
		outboxIDs = append(outboxIDs, rows[i].OutboxID)
	}
	return deliverysql.UniqueInt64s(outboxIDs)
}
