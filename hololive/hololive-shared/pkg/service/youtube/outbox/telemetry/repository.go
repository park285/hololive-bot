package telemetry

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/kapu/hololive-shared/pkg/dbx"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/deliverysql"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/timeline"
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
		return fmt.Errorf("prepare rows: %w", err)
	}

	if err := r.EnqueuePrepared(ctx, prepared); err != nil {
		return fmt.Errorf("enqueue prepared: %w", err)
	}

	return nil
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
		row, ok := prepareDeliveryTelemetryRow(&rows[i], now)
		if !ok {
			continue
		}

		// post_id는 기록 주체(TransitionStore)가 검증한 logical key다. content_id로 채우지 않고 누락을 오류로 드러낸다.
		if row.PostID == "" {
			return nil, fmt.Errorf("delivery %d attempt %d: post_id is empty", row.DeliveryID, row.AttemptOrdinal)
		}

		if row.DeliveryPath == "" {
			return nil, fmt.Errorf("delivery %d attempt %d: delivery_path is empty", row.DeliveryID, row.AttemptOrdinal)
		}

		normalized = append(normalized, row)
	}

	if len(normalized) == 0 {
		return nil, nil
	}

	if err := r.enrichRows(ctx, normalized); err != nil {
		return nil, fmt.Errorf("enrich rows: %w", err)
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

	row.ContentID = strings.TrimSpace(row.ContentID)
	row.PostID = strings.TrimSpace(row.PostID)
	row.DeliveryPath = strings.TrimSpace(row.DeliveryPath)
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
		return errors.New("enqueue delivery telemetry: db is nil")
	}

	for start := 0; start < len(rows); start += enqueueTelemetryChunkSize {
		end := min(start+enqueueTelemetryChunkSize, len(rows))
		if err := r.enqueuePreparedChunk(ctx, rows[start:end]); err != nil {
			return fmt.Errorf("enqueue prepared chunk: %w", err)
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

	// (delivery_id, attempt_ordinal) 중복을 건너뛰던 ON CONFLICT DO NOTHING은 지웠다. 시도는 lifecycle 전이 트랜잭션이 한
	// 번만 기록하므로 중복은 결함이고, unique 위반으로 드러나 그 트랜잭션을 rollback한다
	// (DEC-20260926-hololive-delivery-telemetry-single-path).

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

// FetchAndLockPending은 기록할 telemetry 행을 골라 lease(locked_at)를 한 문장으로 획득한다.
// 만료(lockTimeout)가 지난 lease의 행은 다시 획득할 수 있고, 다른 인스턴스가 잡은 행은 건너뛴다.
func (r *Repository) FetchAndLockPending(ctx context.Context, batchSize int, lockTimeout time.Duration) ([]domain.YouTubeNotificationDeliveryTelemetry, error) {
	if batchSize <= 0 {
		return nil, nil
	}

	now := time.Now().UTC()
	lockExpiry := now.Add(-lockTimeout)

	rows, err := r.db.Query(ctx, mustSQL("repository_fetch_lock_pending.sql"), now, lockExpiry, batchSize)
	if err != nil {
		return nil, fmt.Errorf("fetch and lock pending delivery telemetry: %w", err)
	}

	locked, err := pgx.CollectRows(rows, scanTelemetryRow)
	if err != nil {
		return nil, fmt.Errorf("fetch and lock pending delivery telemetry: collect rows: %w", err)
	}

	if len(locked) == 0 {
		return nil, nil
	}

	// UPDATE ... RETURNING은 순서를 보장하지 않으므로 기록 순서를 (event_at, id)로 되돌린다.
	slices.SortFunc(locked, func(a, b domain.YouTubeNotificationDeliveryTelemetry) int {
		return cmp.Or(a.EventAt.Compare(b.EventAt), cmp.Compare(a.ID, b.ID))
	})

	if err := r.refreshLockedRows(ctx, locked); err != nil {
		return nil, fmt.Errorf("refresh locked rows: %w", err)
	}

	return locked, nil
}

func (r *Repository) MarkLoggedBatch(ctx context.Context, ids []int64) error {
	uniqueIDs := deliverysql.UniqueInt64s(ids)
	if len(uniqueIDs) == 0 {
		return nil
	}

	now := time.Now().UTC()
	args := append([]any{now}, dbx.AnyArgs(uniqueIDs)...)

	if _, err := dbx.ExecSQL(ctx, r.db, "mark delivery telemetry logged", mustSQL("repository_0279_06.sql")+deliverysql.DeliveryInClause("id", len(uniqueIDs))+`
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
	args := append([]any{nextAttemptAt, deliverysql.TruncateString(errMsg, 500)}, dbx.AnyArgs(uniqueIDs)...)

	if _, err := dbx.ExecSQL(ctx, r.db, "mark delivery telemetry retry", mustSQL("repository_0299_07.sql")+deliverysql.DeliveryInClause("id", len(uniqueIDs))+`
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

	if _, err := dbx.ExecSQL(ctx, r.db, "refresh locked delivery telemetry rows", mustSQL("repository_0343_08.sql"), ids, actualPublishedAt, alarmSentAt, alarmLatencyMillis, detectedAt); err != nil {
		return fmt.Errorf("exec delivery SQL: %w", err)
	}

	return nil
}

const retentionDeleteBatchSize = 1000

func (r *Repository) DeleteLoggedBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	out, err := r.deleteLoggedBeforeInBatches(ctx, cutoff, retentionDeleteBatchSize)
	if err != nil {
		return out, fmt.Errorf("delete logged before in batches: %w", err)
	}

	return out, nil
}

func (r *Repository) deleteLoggedBeforeInBatches(ctx context.Context, cutoff time.Time, batchSize int) (int64, error) {
	if r == nil || r.db == nil || cutoff.IsZero() {
		return 0, nil
	}

	var total int64

	for {
		deleted, done, err := r.deleteLoggedBeforeBatch(ctx, cutoff, batchSize)

		total += deleted

		if err != nil {
			return total, fmt.Errorf("delete logged before batch: %w", err)
		}

		if done {
			return total, nil
		}
	}
}

func (r *Repository) deleteLoggedBeforeBatch(ctx context.Context, cutoff time.Time, batchSize int) (deleted int64, done bool, err error) {
	tag, err := r.db.Exec(ctx, mustSQL("repository_0364_09.sql"), cutoff.UTC(), batchSize)
	if err != nil {
		return 0, true, fmt.Errorf("delete delivery telemetry before cutoff: %w", err)
	}

	deleted = tag.RowsAffected()
	if deleted < int64(batchSize) {
		return deleted, true, nil
	}

	if err := deliverysql.YieldBetweenDeleteBatches(ctx); err != nil {
		return deleted, true, fmt.Errorf("yield between delete batches: %w", err)
	}

	return deleted, false, nil
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
