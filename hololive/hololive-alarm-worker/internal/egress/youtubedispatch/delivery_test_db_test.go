package youtubedispatch

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode"

	"github.com/georgysavva/scany/v2/pgxscan"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/deliverysql"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/store"
)

type deliveryTestSQLResult struct {
	Error        error
	RowsAffected int64
}

// deliveryTestDB는 과거 fluent ORM식 shim 타입을 대체하기 위한 호환 alias입니다.
// 메서드는 의도적으로 두지 않습니다. 테스트는 newDeliveryPool + 명시 helper를 사용합니다.
type deliveryTestDB = pgxpool.Pool

func newDeliveryPool(tb testing.TB) *pgxpool.Pool {
	tb.Helper()

	pool := dbtest.NewPool(tb)

	for _, statement := range []string{
		`ALTER TABLE youtube_notification_delivery DROP CONSTRAINT IF EXISTS youtube_notification_delivery_outbox_id_fkey`,
		`ALTER TABLE youtube_notification_delivery_telemetry DROP CONSTRAINT IF EXISTS youtube_notification_delivery_telemetry_outbox_id_fkey`,
	} {
		if _, err := pool.Exec(tb.Context(), statement); err != nil {
			tb.Fatalf("delivery test db: relax legacy unit-test constraint: %v", err)
		}
	}

	return pool
}

func newDeliveryExecModePool(t *testing.T, pool *pgxpool.Pool) *pgxpool.Pool {
	t.Helper()

	cfg := pool.Config()

	cfg.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeExec

	execPool, err := pgxpool.NewWithConfig(t.Context(), cfg)
	require.NoError(t, err)
	t.Cleanup(execPool.Close)

	return execPool
}

func insertDeliveryTestRows(pool *pgxpool.Pool, value any) deliveryTestSQLResult {
	rows, err := insertDeliveryTestRowsContext(context.Background(), pool, value)
	return deliveryTestSQLResult{Error: err, RowsAffected: rows}
}

func firstDeliveryTestRow(pool *pgxpool.Pool, dest any, conds ...any) deliveryTestSQLResult {
	err := firstDeliveryTestRowContext(context.Background(), pool, dest, conds...)
	return deliveryTestSQLResult{Error: err}
}

func firstDeliveryTestRowWhere(pool *pgxpool.Pool, dest any, where string, args ...any) deliveryTestSQLResult {
	all := append([]any{where}, args...)
	err := firstDeliveryTestRowContext(context.Background(), pool, dest, all...)

	return deliveryTestSQLResult{Error: err}
}

func findDeliveryTestRows(pool *pgxpool.Pool, dest any) deliveryTestSQLResult {
	err := findDeliveryTestRowsContext(context.Background(), pool, dest, "", "")
	return deliveryTestSQLResult{Error: err}
}

func findDeliveryTestRowsOrdered(pool *pgxpool.Pool, dest any, order string) deliveryTestSQLResult {
	err := findDeliveryTestRowsContext(context.Background(), pool, dest, "", order)
	return deliveryTestSQLResult{Error: err}
}

func findDeliveryTestRowsOrderedWhere(pool *pgxpool.Pool, dest any, order, where string, args ...any) deliveryTestSQLResult {
	err := findDeliveryTestRowsContext(context.Background(), pool, dest, where, order, args...)
	return deliveryTestSQLResult{Error: err}
}

func countDeliveryTestRowsWhere(pool *pgxpool.Pool, model any, dest *int64, where string, args ...any) deliveryTestSQLResult {
	table := deliveryTestTableForModel(model)
	if table == "" {
		return deliveryTestSQLResult{Error: fmt.Errorf("count rows: unsupported model %T", model)}
	}

	query := "SELECT COUNT(*) FROM " + table

	if strings.TrimSpace(where) != "" {
		query += " WHERE " + where
	}

	err := pool.QueryRow(context.Background(), deliverysql.PostgresPlaceholders(query), args...).Scan(dest)

	return deliveryTestSQLResult{Error: err}
}

func updateDeliveryTestRowsWhere(pool *pgxpool.Pool, model any, values map[string]any, where string, args ...any) deliveryTestSQLResult {
	table := deliveryTestTableForModel(model)
	if table == "" {
		return deliveryTestSQLResult{Error: fmt.Errorf("update rows: unsupported model %T", model)}
	}

	if len(values) == 0 {
		return deliveryTestSQLResult{}
	}

	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}

	slices.Sort(keys)

	assignments := make([]string, 0, len(keys))
	queryArgs := make([]any, 0, len(values)+len(args))

	for _, key := range keys {
		assignments = append(assignments, deliveryTestUpdateAssignment(key))
		queryArgs = append(queryArgs, values[key])
	}

	query := "UPDATE " + table + " SET " + strings.Join(assignments, ", ")

	if strings.TrimSpace(where) != "" {
		query += " WHERE " + where
	}

	queryArgs = append(queryArgs, args...)

	tag, err := pool.Exec(context.Background(), deliverysql.PostgresPlaceholders(query), queryArgs...)

	return deliveryTestSQLResult{Error: err, RowsAffected: tag.RowsAffected()}
}

func firstDeliveryTestRowContext(ctx context.Context, pool *pgxpool.Pool, dest any, conds ...any) error {
	table := deliveryTestTableForDest(dest)
	if table == "" {
		return fmt.Errorf("first row: unsupported dest %T", dest)
	}

	query := "SELECT " + deliveryTestSelectColumns(table) + " FROM " + table
	args := []any(nil)

	if len(conds) > 0 {
		switch cond := conds[0].(type) {
		case string:
			query += " WHERE " + cond

			args = append(args, conds[1:]...)
		default:
			query += " WHERE id = ?"

			args = []any{cond}
		}
	}

	query += " LIMIT 1"

	if err := pgxscan.Get(ctx, pool, dest, deliverysql.PostgresPlaceholders(query), args...); err != nil {
		return fmt.Errorf("get: %w", err)
	}

	return nil
}

func findDeliveryTestRowsContext(ctx context.Context, pool *pgxpool.Pool, dest any, where, order string, args ...any) error {
	table := deliveryTestTableForDest(dest)
	if table == "" {
		return fmt.Errorf("find rows: unsupported dest %T", dest)
	}

	query := "SELECT " + deliveryTestSelectColumns(table) + " FROM " + table

	if strings.TrimSpace(where) != "" {
		query += " WHERE " + where
	}

	if strings.TrimSpace(order) != "" {
		query += " ORDER BY " + order
	}

	if err := pgxscan.Select(ctx, pool, dest, deliverysql.PostgresPlaceholders(query), args...); err != nil {
		return fmt.Errorf("select: %w", err)
	}

	return nil
}

func insertDeliveryTestRowsContext(ctx context.Context, pool *pgxpool.Pool, value any) (int64, error) {
	for _, dispatch := range []func(context.Context, *pgxpool.Pool, any) (int64, bool, error){
		insertDeliveryTestOutboxRowsContext,
		insertDeliveryTestDeliveryRowsContext,
		insertDeliveryTestTrackingRowsContext,
		insertDeliveryTestAlarmStateRowsContext,
		insertDeliveryTestTelemetryRowsContext,
		insertDeliveryTestAlarmRowsContext,
	} {
		affected, matched, err := dispatch(ctx, pool, value)
		if !matched {
			continue
		}

		if err != nil {
			return affected, fmt.Errorf("insert delivery test rows: %w", err)
		}

		return affected, nil
	}

	out, err := insertDeliveryTestRowsGeneric(ctx, pool, value)
	if err != nil {
		return out, fmt.Errorf("insert delivery test rows generic: %w", err)
	}

	return out, nil
}

func insertDeliveryTestOutboxRowsContext(ctx context.Context, pool *pgxpool.Pool, value any) (int64, bool, error) {
	var (
		affected int64
		err      error
	)

	switch rows := value.(type) {
	case *domain.YouTubeNotificationOutbox:
		affected, err = insertDomainOutbox(ctx, pool, rows)
	case domain.YouTubeNotificationOutbox:
		affected, err = insertDomainOutbox(ctx, pool, &rows)
	case []domain.YouTubeNotificationOutbox:
		affected, err = insertDomainOutboxSlice(ctx, pool, rows)
	case *[]domain.YouTubeNotificationOutbox:
		affected, err = insertDomainOutboxSlice(ctx, pool, *rows)
	case []*domain.YouTubeNotificationOutbox:
		affected, err = insertDeliveryTestPtrSlice(ctx, pool, rows, insertDomainOutbox)
	case *deliveryTestOutboxModel:
		affected, err = insertTestOutboxModel(ctx, pool, rows)
	case deliveryTestOutboxModel:
		affected, err = insertTestOutboxModel(ctx, pool, &rows)
	case []deliveryTestOutboxModel:
		affected, err = insertDeliveryTestValueSlice(ctx, pool, rows, insertTestOutboxModel)
	case *[]deliveryTestOutboxModel:
		affected, err = insertDeliveryTestValueSlice(ctx, pool, *rows, insertTestOutboxModel)
	default:
		return 0, false, nil
	}

	if err != nil {
		return affected, true, fmt.Errorf("insert outbox rows: %w", err)
	}

	return affected, true, nil
}

func insertDeliveryTestDeliveryRowsContext(ctx context.Context, pool *pgxpool.Pool, value any) (int64, bool, error) {
	var (
		affected int64
		err      error
	)

	switch rows := value.(type) {
	case *domain.YouTubeNotificationDelivery:
		affected, err = insertDomainDelivery(ctx, pool, rows)
	case domain.YouTubeNotificationDelivery:
		affected, err = insertDomainDelivery(ctx, pool, &rows)
	case []domain.YouTubeNotificationDelivery:
		affected, err = insertDeliveryTestValueSlice(ctx, pool, rows, insertDomainDelivery)
	case *[]domain.YouTubeNotificationDelivery:
		affected, err = insertDeliveryTestValueSlice(ctx, pool, *rows, insertDomainDelivery)
	case []*domain.YouTubeNotificationDelivery:
		affected, err = insertDeliveryTestPtrSlice(ctx, pool, rows, insertDomainDelivery)
	case *deliveryTestDeliveryModel:
		affected, err = insertTestDeliveryModel(ctx, pool, rows)
	case deliveryTestDeliveryModel:
		affected, err = insertTestDeliveryModel(ctx, pool, &rows)
	case []deliveryTestDeliveryModel:
		affected, err = insertDeliveryTestValueSlice(ctx, pool, rows, insertTestDeliveryModel)
	case *[]deliveryTestDeliveryModel:
		affected, err = insertDeliveryTestValueSlice(ctx, pool, *rows, insertTestDeliveryModel)
	default:
		return 0, false, nil
	}

	if err != nil {
		return affected, true, fmt.Errorf("insert delivery rows: %w", err)
	}

	return affected, true, nil
}

func insertDeliveryTestTrackingRowsContext(ctx context.Context, pool *pgxpool.Pool, value any) (int64, bool, error) {
	var (
		affected int64
		err      error
	)

	switch rows := value.(type) {
	case *domain.YouTubeContentAlarmTracking:
		affected, err = insertDomainTracking(ctx, pool, rows)
	case domain.YouTubeContentAlarmTracking:
		affected, err = insertDomainTracking(ctx, pool, &rows)
	case []domain.YouTubeContentAlarmTracking:
		affected, err = insertDeliveryTestValueSlice(ctx, pool, rows, insertDomainTracking)
	case *[]domain.YouTubeContentAlarmTracking:
		affected, err = insertDeliveryTestValueSlice(ctx, pool, *rows, insertDomainTracking)
	case *deliveryTestTrackingModel:
		affected, err = insertTestTrackingModel(ctx, pool, rows)
	case deliveryTestTrackingModel:
		affected, err = insertTestTrackingModel(ctx, pool, &rows)
	case []deliveryTestTrackingModel:
		affected, err = insertDeliveryTestValueSlice(ctx, pool, rows, insertTestTrackingModel)
	case *[]deliveryTestTrackingModel:
		affected, err = insertDeliveryTestValueSlice(ctx, pool, *rows, insertTestTrackingModel)
	default:
		return 0, false, nil
	}

	if err != nil {
		return affected, true, fmt.Errorf("insert tracking rows: %w", err)
	}

	return affected, true, nil
}

func insertDeliveryTestAlarmStateRowsContext(ctx context.Context, pool *pgxpool.Pool, value any) (int64, bool, error) {
	var (
		affected int64
		err      error
	)

	switch rows := value.(type) {
	case *domain.YouTubeCommunityShortsAlarmState:
		affected, err = insertDomainAlarmState(ctx, pool, rows)
	case domain.YouTubeCommunityShortsAlarmState:
		affected, err = insertDomainAlarmState(ctx, pool, &rows)
	case []domain.YouTubeCommunityShortsAlarmState:
		affected, err = insertDeliveryTestValueSlice(ctx, pool, rows, insertDomainAlarmState)
	case *[]domain.YouTubeCommunityShortsAlarmState:
		affected, err = insertDeliveryTestValueSlice(ctx, pool, *rows, insertDomainAlarmState)
	default:
		return 0, false, nil
	}

	if err != nil {
		return affected, true, fmt.Errorf("insert alarm state rows: %w", err)
	}

	return affected, true, nil
}

func insertDeliveryTestTelemetryRowsContext(ctx context.Context, pool *pgxpool.Pool, value any) (int64, bool, error) {
	var (
		affected int64
		err      error
	)

	switch rows := value.(type) {
	case *domain.YouTubeNotificationDeliveryTelemetry:
		affected, err = insertDomainTelemetry(ctx, pool, rows)
	case domain.YouTubeNotificationDeliveryTelemetry:
		affected, err = insertDomainTelemetry(ctx, pool, &rows)
	case []domain.YouTubeNotificationDeliveryTelemetry:
		affected, err = insertDeliveryTestValueSlice(ctx, pool, rows, insertDomainTelemetry)
	case *[]domain.YouTubeNotificationDeliveryTelemetry:
		affected, err = insertDeliveryTestValueSlice(ctx, pool, *rows, insertDomainTelemetry)
	default:
		return 0, false, nil
	}

	if err != nil {
		return affected, true, fmt.Errorf("insert telemetry rows: %w", err)
	}

	return affected, true, nil
}

func insertDeliveryTestAlarmRowsContext(ctx context.Context, pool *pgxpool.Pool, value any) (int64, bool, error) {
	var (
		affected int64
		err      error
	)

	switch rows := value.(type) {
	case *domain.Alarm:
		affected, err = insertDeliveryTestAlarm(ctx, pool, rows)
	case []*domain.Alarm:
		affected, err = insertDeliveryTestPtrSlice(ctx, pool, rows, insertDeliveryTestAlarm)
	case []domain.Alarm:
		affected, err = insertDeliveryTestValueSlice(ctx, pool, rows, insertDeliveryTestAlarm)
	case *[]domain.Alarm:
		affected, err = insertDeliveryTestValueSlice(ctx, pool, *rows, insertDeliveryTestAlarm)
	default:
		return 0, false, nil
	}

	if err != nil {
		return affected, true, fmt.Errorf("insert alarm rows: %w", err)
	}

	return affected, true, nil
}

func insertDeliveryTestValueSlice[T any](
	ctx context.Context,
	pool *pgxpool.Pool,
	rows []T,
	insert func(context.Context, *pgxpool.Pool, *T) (int64, error),
) (int64, error) {
	var affected int64

	for i := range rows {
		n, err := insert(ctx, pool, &rows[i])
		if err != nil {
			return affected, fmt.Errorf("insert: %w", err)
		}

		affected += n
	}

	return affected, nil
}

func insertDeliveryTestPtrSlice[T any](
	ctx context.Context,
	pool *pgxpool.Pool,
	rows []*T,
	insert func(context.Context, *pgxpool.Pool, *T) (int64, error),
) (int64, error) {
	var affected int64

	for _, row := range rows {
		n, err := insert(ctx, pool, row)
		if err != nil {
			return affected, fmt.Errorf("insert: %w", err)
		}

		affected += n
	}

	return affected, nil
}

func insertDomainOutboxSlice(ctx context.Context, pool *pgxpool.Pool, rows []domain.YouTubeNotificationOutbox) (int64, error) {
	var affected int64

	for i := range rows {
		n, err := insertDomainOutbox(ctx, pool, &rows[i])
		if err != nil {
			return affected, fmt.Errorf("insert domain outbox: %w", err)
		}

		affected += n
	}

	return affected, nil
}

func normalizeTimePtr(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}

	normalized := value.UTC()

	return &normalized
}

func normalizeRequiredTime(value, fallback time.Time) time.Time {
	if value.IsZero() {
		return fallback
	}

	return value.UTC()
}

func insertDomainOutbox(ctx context.Context, pool *pgxpool.Pool, row *domain.YouTubeNotificationOutbox) (int64, error) {
	if row == nil {
		return 0, nil
	}

	now := time.Now().UTC()

	row.CreatedAt = normalizeRequiredTime(row.CreatedAt, now)
	row.NextAttemptAt = normalizeRequiredTime(row.NextAttemptAt, now)
	row.LockedAt = normalizeTimePtr(row.LockedAt)
	row.SentAt = normalizeTimePtr(row.SentAt)

	if row.Status == "" {
		row.Status = domain.OutboxStatusPending
	}

	if row.ID == 0 {
		err := pool.QueryRow(ctx, `INSERT INTO youtube_notification_outbox (kind, channel_id, content_id, payload, status, attempt_count, next_attempt_at, created_at, locked_at, sent_at, error) VALUES ($1, $2, $3, $4::jsonb, $5, $6, $7, $8, $9, $10, $11) RETURNING id`, row.Kind, row.ChannelID, row.ContentID, row.Payload, row.Status, row.AttemptCount, row.NextAttemptAt, row.CreatedAt, row.LockedAt, row.SentAt, row.Error).Scan(&row.ID)
		if err != nil {
			return 0, fmt.Errorf("scan: %w", err)
		}

		return 1, nil
	}

	tag, err := pool.Exec(ctx, `INSERT INTO youtube_notification_outbox (id, kind, channel_id, content_id, payload, status, attempt_count, next_attempt_at, created_at, locked_at, sent_at, error) VALUES ($1, $2, $3, $4, $5::jsonb, $6, $7, $8, $9, $10, $11, $12)`, row.ID, row.Kind, row.ChannelID, row.ContentID, row.Payload, row.Status, row.AttemptCount, row.NextAttemptAt, row.CreatedAt, row.LockedAt, row.SentAt, row.Error)

	return tag.RowsAffected(), err
}

func insertTestOutboxModel(ctx context.Context, pool *pgxpool.Pool, row *deliveryTestOutboxModel) (int64, error) {
	if row == nil {
		return 0, nil
	}

	dom := domain.YouTubeNotificationOutbox{ID: row.ID, Kind: domain.OutboxKind(row.Kind), ChannelID: row.ChannelID, ContentID: row.ContentID, Payload: row.Payload, Status: domain.OutboxStatus(row.Status), AttemptCount: row.AttemptCount, NextAttemptAt: row.NextAttemptAt, CreatedAt: row.CreatedAt, LockedAt: row.LockedAt, SentAt: row.SentAt, Error: row.Error}
	n, err := insertDomainOutbox(ctx, pool, &dom)

	row.ID = dom.ID

	if err != nil {
		return n, fmt.Errorf("insert test outbox model: %w", err)
	}

	return n, nil
}

func insertDomainDelivery(ctx context.Context, pool *pgxpool.Pool, row *domain.YouTubeNotificationDelivery) (int64, error) {
	if row == nil {
		return 0, nil
	}

	now := time.Now().UTC()

	row.CreatedAt = normalizeRequiredTime(row.CreatedAt, now)
	row.NextAttemptAt = normalizeRequiredTime(row.NextAttemptAt, now)
	row.LockedAt = normalizeTimePtr(row.LockedAt)
	row.SentAt = normalizeTimePtr(row.SentAt)

	if row.Status == "" {
		row.Status = domain.OutboxStatusPending
	}

	if row.ID == 0 {
		err := pool.QueryRow(ctx, `INSERT INTO youtube_notification_delivery (outbox_id, room_id, status, attempt_count, next_attempt_at, created_at, locked_at, sent_at, error) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) RETURNING id`, row.OutboxID, row.RoomID, row.Status, row.AttemptCount, row.NextAttemptAt, row.CreatedAt, row.LockedAt, row.SentAt, row.Error).Scan(&row.ID)
		if err != nil {
			return 0, fmt.Errorf("scan: %w", err)
		}

		return 1, nil
	}

	tag, err := pool.Exec(ctx, `INSERT INTO youtube_notification_delivery (id, outbox_id, room_id, status, attempt_count, next_attempt_at, created_at, locked_at, sent_at, error) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`, row.ID, row.OutboxID, row.RoomID, row.Status, row.AttemptCount, row.NextAttemptAt, row.CreatedAt, row.LockedAt, row.SentAt, row.Error)

	return tag.RowsAffected(), err
}

func insertTestDeliveryModel(ctx context.Context, pool *pgxpool.Pool, row *deliveryTestDeliveryModel) (int64, error) {
	if row == nil {
		return 0, nil
	}

	dom := domain.YouTubeNotificationDelivery{ID: row.ID, OutboxID: row.OutboxID, RoomID: row.RoomID, Status: domain.OutboxStatus(row.Status), AttemptCount: row.AttemptCount, NextAttemptAt: row.NextAttemptAt, CreatedAt: row.CreatedAt, LockedAt: row.LockedAt, SentAt: row.SentAt, Error: row.Error}
	n, err := insertDomainDelivery(ctx, pool, &dom)

	row.ID = dom.ID

	if err != nil {
		return n, fmt.Errorf("insert test delivery model: %w", err)
	}

	return n, nil
}

func insertDomainTracking(ctx context.Context, pool *pgxpool.Pool, row *domain.YouTubeContentAlarmTracking) (int64, error) {
	if row == nil {
		return 0, nil
	}

	now := time.Now().UTC()

	row.CreatedAt = normalizeRequiredTime(row.CreatedAt, now)
	row.UpdatedAt = normalizeRequiredTime(row.UpdatedAt, now)
	row.DetectedAt = normalizeRequiredTime(row.DetectedAt, now)
	row.ActualPublishedAt = normalizeTimePtr(row.ActualPublishedAt)
	row.AlarmSentAt = normalizeTimePtr(row.AlarmSentAt)

	if row.CanonicalContentID == "" {
		canonicalContentID, err := store.CanonicalDeliveryPostID(row.Kind, row.ContentID)
		if err != nil {
			return 0, fmt.Errorf("canonical delivery post id: %w", err)
		}

		row.CanonicalContentID = canonicalContentID
	}

	if row.DeliveryStatus == "" {
		row.DeliveryStatus = domain.YouTubeContentAlarmDeliveryStatusPending
	}

	tag, err := pool.Exec(ctx, `INSERT INTO youtube_content_alarm_tracking (kind, content_id, canonical_content_id, channel_id, actual_published_at, detected_at, alarm_sent_at, alarm_latency_millis, alarm_latency_exceeded, delivery_status, latency_classification_status, delay_source, internal_delay_cause, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)`, row.Kind, row.ContentID, row.CanonicalContentID, row.ChannelID, row.ActualPublishedAt, row.DetectedAt, row.AlarmSentAt, row.AlarmLatencyMillis, row.AlarmLatencyExceeded, row.DeliveryStatus, row.LatencyClassificationStatus, row.DelaySource, row.InternalDelayCause, row.CreatedAt, row.UpdatedAt)

	return tag.RowsAffected(), err
}

func insertTestTrackingModel(ctx context.Context, pool *pgxpool.Pool, row *deliveryTestTrackingModel) (int64, error) {
	if row == nil {
		return 0, nil
	}

	dom := domain.YouTubeContentAlarmTracking{Kind: domain.OutboxKind(row.Kind), ContentID: row.ContentID, CanonicalContentID: row.CanonicalContentID, ChannelID: row.ChannelID, ActualPublishedAt: row.ActualPublishedAt, DetectedAt: row.DetectedAt, AlarmSentAt: row.AlarmSentAt, AlarmLatencyMillis: row.AlarmLatencyMillis, AlarmLatencyExceeded: row.AlarmLatencyExceeded, DeliveryStatus: domain.YouTubeContentAlarmDeliveryStatus(row.DeliveryStatus), LatencyClassificationStatus: row.LatencyClassificationStatus, DelaySource: row.DelaySource, InternalDelayCause: row.InternalDelayCause, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt}
	n, err := insertDomainTracking(ctx, pool, &dom)

	row.CanonicalContentID = dom.CanonicalContentID
	row.CreatedAt = dom.CreatedAt
	row.UpdatedAt = dom.UpdatedAt

	if err != nil {
		return n, fmt.Errorf("insert test tracking model: %w", err)
	}

	return n, nil
}

func insertDomainAlarmState(ctx context.Context, pool *pgxpool.Pool, row *domain.YouTubeCommunityShortsAlarmState) (int64, error) {
	if row == nil {
		return 0, nil
	}

	now := time.Now().UTC()

	row.CreatedAt = normalizeRequiredTime(row.CreatedAt, now)
	row.UpdatedAt = normalizeRequiredTime(row.UpdatedAt, now)
	row.DetectedAt = normalizeRequiredTime(row.DetectedAt, now)
	row.ActualPublishedAt = normalizeTimePtr(row.ActualPublishedAt)
	row.AuthorizedAt = normalizeTimePtr(row.AuthorizedAt)
	row.AlarmSentAt = normalizeTimePtr(row.AlarmSentAt)

	if row.DeliveryStatus == "" {
		row.DeliveryStatus = domain.ResolveYouTubeCommunityShortsAlarmStateStatus(row.AuthorizedAt, row.AlarmSentAt)
	}

	tag, err := pool.Exec(ctx, `INSERT INTO youtube_community_shorts_alarm_states (kind, post_id, content_id, channel_id, actual_published_at, detected_at, authorized_at, alarm_sent_at, delivery_status, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`, row.Kind, row.PostID, row.ContentID, row.ChannelID, row.ActualPublishedAt, row.DetectedAt, row.AuthorizedAt, row.AlarmSentAt, row.DeliveryStatus, row.CreatedAt, row.UpdatedAt)

	return tag.RowsAffected(), err
}

func insertDomainTelemetry(ctx context.Context, pool *pgxpool.Pool, row *domain.YouTubeNotificationDeliveryTelemetry) (int64, error) {
	if row == nil {
		return 0, nil
	}

	now := time.Now().UTC()

	row.EventAt = normalizeRequiredTime(row.EventAt, now)
	row.NextAttemptAt = normalizeRequiredTime(row.NextAttemptAt, now)
	row.CreatedAt = normalizeRequiredTime(row.CreatedAt, now)
	row.ActualPublishedAt = normalizeTimePtr(row.ActualPublishedAt)
	row.AlarmSentAt = normalizeTimePtr(row.AlarmSentAt)
	row.DetectedAt = normalizeTimePtr(row.DetectedAt)
	row.AttemptStartedAt = normalizeTimePtr(row.AttemptStartedAt)
	row.AttemptFinishedAt = normalizeTimePtr(row.AttemptFinishedAt)
	row.LockedAt = normalizeTimePtr(row.LockedAt)
	row.LoggedAt = normalizeTimePtr(row.LoggedAt)

	if row.ID == 0 {
		err := pool.QueryRow(ctx, `INSERT INTO youtube_notification_delivery_telemetry (delivery_id, attempt_ordinal, outbox_id, channel_id, content_id, post_id, room_id, alarm_type, actual_published_at, alarm_sent_at, alarm_latency_millis, detected_at, dedupe_key, delivery_path, delivery_mode, send_result, failure_reason, attempt_started_at, attempt_finished_at, event_at, next_attempt_at, created_at, locked_at, logged_at, error) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24, $25) RETURNING id`, row.DeliveryID, row.AttemptOrdinal, row.OutboxID, row.ChannelID, row.ContentID, row.PostID, row.RoomID, row.AlarmType, row.ActualPublishedAt, row.AlarmSentAt, row.AlarmLatencyMillis, row.DetectedAt, row.DedupeKey, row.DeliveryPath, row.DeliveryMode, row.SendResult, row.FailureReason, row.AttemptStartedAt, row.AttemptFinishedAt, row.EventAt, row.NextAttemptAt, row.CreatedAt, row.LockedAt, row.LoggedAt, row.Error).Scan(&row.ID)
		if err != nil {
			return 0, fmt.Errorf("scan: %w", err)
		}

		return 1, nil
	}

	tag, err := pool.Exec(ctx, `INSERT INTO youtube_notification_delivery_telemetry (id, delivery_id, attempt_ordinal, outbox_id, channel_id, content_id, post_id, room_id, alarm_type, actual_published_at, alarm_sent_at, alarm_latency_millis, detected_at, dedupe_key, delivery_path, delivery_mode, send_result, failure_reason, attempt_started_at, attempt_finished_at, event_at, next_attempt_at, created_at, locked_at, logged_at, error) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24, $25, $26)`, row.ID, row.DeliveryID, row.AttemptOrdinal, row.OutboxID, row.ChannelID, row.ContentID, row.PostID, row.RoomID, row.AlarmType, row.ActualPublishedAt, row.AlarmSentAt, row.AlarmLatencyMillis, row.DetectedAt, row.DedupeKey, row.DeliveryPath, row.DeliveryMode, row.SendResult, row.FailureReason, row.AttemptStartedAt, row.AttemptFinishedAt, row.EventAt, row.NextAttemptAt, row.CreatedAt, row.LockedAt, row.LoggedAt, row.Error)

	return tag.RowsAffected(), err
}

func insertDeliveryTestAlarm(ctx context.Context, pool *pgxpool.Pool, alarm *domain.Alarm) (int64, error) {
	if alarm == nil {
		return 0, nil
	}

	if alarm.CreatedAt.IsZero() {
		alarm.CreatedAt = time.Now().UTC()
	} else {
		alarm.CreatedAt = alarm.CreatedAt.UTC()
	}

	alarmTypes, err := alarm.AlarmTypes.Value()
	if err != nil {
		return 0, fmt.Errorf("value: %w", err)
	}

	err = pool.QueryRow(ctx, `INSERT INTO alarms (room_id, user_id, channel_id, member_name, room_name, user_name, alarm_types, created_at) VALUES ($1, $2, $3, $4, $5, $6, $7::alarm_type[], $8) RETURNING id`, alarm.RoomID, alarm.UserID, alarm.ChannelID, alarm.MemberName, alarm.RoomName, alarm.UserName, alarmTypes, alarm.CreatedAt).Scan(&alarm.ID)
	if err != nil {
		return 0, fmt.Errorf("scan: %w", err)
	}

	return 1, nil
}

func deliveryTestTableForDest(dest any) string {
	switch dest.(type) {
	case *domain.YouTubeNotificationOutbox, *[]domain.YouTubeNotificationOutbox, *[]*domain.YouTubeNotificationOutbox, *deliveryTestOutboxModel, *[]deliveryTestOutboxModel:
		return testTableOutbox
	case *domain.YouTubeNotificationDelivery, *[]domain.YouTubeNotificationDelivery, *[]*domain.YouTubeNotificationDelivery, *deliveryTestDeliveryModel, *[]deliveryTestDeliveryModel:
		return testTableDelivery
	case *domain.YouTubeContentAlarmTracking, *[]domain.YouTubeContentAlarmTracking, *deliveryTestTrackingModel, *[]deliveryTestTrackingModel:
		return testTableContentAlarmTracking
	case *domain.YouTubeCommunityShortsAlarmState, *[]domain.YouTubeCommunityShortsAlarmState:
		return "youtube_community_shorts_alarm_states"
	case *domain.YouTubeNotificationDeliveryTelemetry, *[]domain.YouTubeNotificationDeliveryTelemetry:
		return testTableDeliveryTelemetry
	default:
		return deliveryTestTableName(dest)
	}
}

func deliveryTestTableForModel(model any) string {
	switch model.(type) {
	case *domain.YouTubeNotificationOutbox, domain.YouTubeNotificationOutbox, *deliveryTestOutboxModel, deliveryTestOutboxModel:
		return testTableOutbox
	case *domain.YouTubeNotificationDelivery, domain.YouTubeNotificationDelivery, *deliveryTestDeliveryModel, deliveryTestDeliveryModel:
		return testTableDelivery
	case *domain.YouTubeContentAlarmTracking, domain.YouTubeContentAlarmTracking, *deliveryTestTrackingModel, deliveryTestTrackingModel:
		return testTableContentAlarmTracking
	case *domain.YouTubeCommunityShortsAlarmState, domain.YouTubeCommunityShortsAlarmState:
		return "youtube_community_shorts_alarm_states"
	case *domain.YouTubeNotificationDeliveryTelemetry, domain.YouTubeNotificationDeliveryTelemetry:
		return testTableDeliveryTelemetry
	case *domain.Alarm, domain.Alarm:
		return "alarms"
	default:
		return deliveryTestTableForDest(model)
	}
}

func deliveryTestUpdateAssignment(column string) string {
	switch column {
	case "actual_published_at", "alarm_sent_at", "attempt_finished_at", "attempt_started_at", "authorized_at", "created_at", "detected_at", "event_at", "locked_at", "logged_at", "next_attempt_at", "sent_at", "updated_at":
		return fmt.Sprintf("%s = ?::timestamptz", column)
	default:
		return fmt.Sprintf("%s = ?", column)
	}
}

func deliveryTestSelectColumns(table string) string {
	switch table {
	case testTableOutbox:
		return "id, kind, channel_id, content_id, payload::text AS payload, status, attempt_count, next_attempt_at, created_at, locked_at, sent_at, COALESCE(error, '') AS error"
	case testTableDelivery:
		return "id, outbox_id, room_id, status, attempt_count, next_attempt_at, created_at, locked_at, sent_at, COALESCE(error, '') AS error"
	case testTableContentAlarmTracking:
		return "kind, content_id, COALESCE(canonical_content_id, '') AS canonical_content_id, channel_id, actual_published_at, detected_at, alarm_sent_at, alarm_latency_millis, alarm_latency_exceeded, COALESCE(delivery_status, '') AS delivery_status, COALESCE(latency_classification_status, '') AS latency_classification_status, COALESCE(delay_source, '') AS delay_source, COALESCE(internal_delay_cause, '') AS internal_delay_cause, created_at, updated_at"
	case "youtube_community_shorts_alarm_states":
		return "kind, post_id, content_id, channel_id, actual_published_at, detected_at, authorized_at, alarm_sent_at, delivery_status, created_at, updated_at"
	case testTableDeliveryTelemetry:
		return "id, delivery_id, attempt_ordinal, outbox_id, channel_id, content_id, COALESCE(post_id, '') AS post_id, room_id, alarm_type, actual_published_at, alarm_sent_at, alarm_latency_millis, detected_at, COALESCE(dedupe_key, '') AS dedupe_key, COALESCE(delivery_path, '') AS delivery_path, COALESCE(delivery_mode, '') AS delivery_mode, COALESCE(send_result, '') AS send_result, COALESCE(failure_reason, '') AS failure_reason, attempt_started_at, attempt_finished_at, event_at, next_attempt_at, created_at, locked_at, logged_at, COALESCE(error, '') AS error"
	default:
		return "*"
	}
}

// insertDeliveryTestRowsGeneric은 위의 typed switch가 열거하지 않는 test-local 모델
// (deliveryTelemetryTest* 구조체)을 위한 reflection 기반 fallback이다. 이 함수는 read 경로의 scany
// reflection을 그대로 따라, 컬럼명은 `db`/`json` tag(없으면 snake_case)에서, 테이블명은
// TableName()에서 가져온다.
func insertDeliveryTestRowsGeneric(ctx context.Context, pool *pgxpool.Pool, value any) (int64, error) {
	v, ok := deliveryTestCreateValue(value)
	if !ok {
		return 0, nil
	}

	if v.Kind() == reflect.Slice {
		out, err := insertDeliveryTestGenericSlice(ctx, pool, v)
		if err != nil {
			return out, fmt.Errorf("insert delivery test generic slice: %w", err)
		}

		return out, nil
	}

	if v.Kind() != reflect.Struct {
		return 0, fmt.Errorf("unsupported create value: %T", value)
	}

	deliveryTestApplyCreateDefaults(v)

	table := deliveryTestTableName(value)
	if table == "" {
		return 0, fmt.Errorf("unsupported create table for %T", value)
	}

	insert := buildDeliveryTestGenericInsert(v)
	if len(insert.columns) == 0 {
		return 0, fmt.Errorf("no insert columns for %T", value)
	}

	query := "INSERT INTO " + table + " (" + strings.Join(insert.columns, ", ") + ") VALUES (" + strings.Join(insert.placeholders, ", ") + ")"
	if insert.omitID && insert.idField.IsValid() && insert.idField.CanSet() {
		query += " RETURNING id"
		if err := pool.QueryRow(ctx, query, insert.args...).Scan(insert.idField.Addr().Interface()); err != nil {
			return 0, fmt.Errorf("scan: %w", err)
		}

		return 1, nil
	}

	tag, err := pool.Exec(ctx, query, insert.args...)
	if err != nil {
		return 0, fmt.Errorf("exec: %w", err)
	}

	return tag.RowsAffected(), nil
}

func deliveryTestCreateValue(value any) (reflect.Value, bool) {
	v := reflect.ValueOf(value)
	if !v.IsValid() {
		return reflect.Value{}, false
	}

	if v.Kind() != reflect.Pointer {
		return v, true
	}

	if v.IsNil() {
		return reflect.Value{}, false
	}

	return v.Elem(), true
}

func insertDeliveryTestGenericSlice(ctx context.Context, pool *pgxpool.Pool, v reflect.Value) (int64, error) {
	var rows int64

	for i := range v.Len() {
		affected, err := insertDeliveryTestGenericSliceItem(ctx, pool, v.Index(i))
		if err != nil {
			return rows, fmt.Errorf("insert delivery test generic slice item: %w", err)
		}

		rows += affected
	}

	return rows, nil
}

func insertDeliveryTestGenericSliceItem(ctx context.Context, pool *pgxpool.Pool, item reflect.Value) (int64, error) {
	if item.Kind() == reflect.Pointer {
		out, err := insertDeliveryTestRowsGeneric(ctx, pool, item.Interface())
		if err != nil {
			return out, fmt.Errorf("insert delivery test rows generic: %w", err)
		}

		return out, nil
	}

	out, err := insertDeliveryTestRowsGeneric(ctx, pool, item.Addr().Interface())
	if err != nil {
		return out, fmt.Errorf("insert delivery test rows generic: %w", err)
	}

	return out, nil
}

type deliveryTestGenericInsert struct {
	columns      []string
	placeholders []string
	args         []any
	idField      reflect.Value
	omitID       bool
}

func buildDeliveryTestGenericInsert(v reflect.Value) deliveryTestGenericInsert {
	insert := deliveryTestGenericInsert{
		columns:      make([]string, 0, v.NumField()),
		placeholders: make([]string, 0, v.NumField()),
		args:         make([]any, 0, v.NumField()),
	}
	for i := range v.NumField() {
		field := v.Type().Field(i)
		insert.addField(&field, v.Field(i))
	}

	return insert
}

func (insert *deliveryTestGenericInsert) addField(field *reflect.StructField, valueField reflect.Value) {
	if field.PkgPath != "" || field.Anonymous {
		return
	}

	column, ok := deliveryTestColumnName(field)
	if !ok {
		return
	}

	if strings.EqualFold(column, "id") && valueField.IsZero() {
		insert.idField = valueField
		insert.omitID = true

		return
	}

	insert.columns = append(insert.columns, column)
	insert.placeholders = append(insert.placeholders, fmt.Sprintf("$%d", len(insert.args)+1))
	insert.args = append(insert.args, valueField.Interface())
}

func deliveryTestApplyCreateDefaults(v reflect.Value) {
	now := time.Now().UTC()
	timeType := reflect.TypeFor[time.Time]()

	for i := range v.NumField() {
		field := v.Type().Field(i)
		deliveryTestApplyFieldCreateDefault(&field, v.Field(i), timeType, now)
	}

	deliveryTestApplyIdentityCreateDefaults(v)
	deliveryTestApplyStatusCreateDefaults(v)
}

func deliveryTestApplyFieldCreateDefault(field *reflect.StructField, value reflect.Value, timeType reflect.Type, now time.Time) {
	if field.PkgPath != "" || !value.CanSet() {
		return
	}

	deliveryTestNormalizeTimeField(value, timeType)
	deliveryTestApplyNamedTimeDefault(field.Name, value, now)
}

func deliveryTestNormalizeTimeField(value reflect.Value, timeType reflect.Type) {
	if value.Type() == timeType {
		if t, ok := reflect.TypeAssert[time.Time](value); ok && !t.IsZero() {
			value.Set(reflect.ValueOf(t.UTC()))
		}
	}

	if value.Kind() == reflect.Pointer && value.Type().Elem() == timeType && !value.IsNil() {
		if t, ok := reflect.TypeAssert[time.Time](value.Elem()); ok {
			utc := t.UTC()
			value.Set(reflect.ValueOf(&utc))
		}
	}
}

func deliveryTestApplyNamedTimeDefault(name string, value reflect.Value, now time.Time) {
	if name != "CreatedAt" && name != "UpdatedAt" && name != "NextAttemptAt" {
		return
	}

	if t, ok := reflect.TypeAssert[time.Time](value); ok && t.IsZero() {
		value.Set(reflect.ValueOf(now))
	}
}

func deliveryTestApplyIdentityCreateDefaults(v reflect.Value) {
	contentID := v.FieldByName("ContentID")
	canonicalContentID := v.FieldByName("CanonicalContentID")

	if contentID.IsValid() && canonicalContentID.IsValid() &&
		contentID.Kind() == reflect.String && canonicalContentID.Kind() == reflect.String &&
		canonicalContentID.CanSet() && canonicalContentID.String() == "" {
		kind := v.FieldByName("Kind")
		if kind.IsValid() && kind.Kind() == reflect.String {
			canonicalContentID.SetString(mustCanonicalDeliveryPostID(domain.OutboxKind(kind.String()), contentID.String()))

			return
		}

		canonicalContentID.SetString(contentID.String())
	}
}

func mustCanonicalDeliveryPostID(kind domain.OutboxKind, contentID string) string {
	canonicalPostID, err := store.CanonicalDeliveryPostID(kind, contentID)
	if err != nil {
		panic(err)
	}

	return canonicalPostID
}

func deliveryTestApplyStatusCreateDefaults(v reflect.Value) {
	deliveryStatus := v.FieldByName("DeliveryStatus")
	if deliveryStatus.IsValid() && deliveryStatus.CanSet() && deliveryStatus.Kind() == reflect.String && deliveryStatus.String() == "" {
		deliveryStatus.SetString("PENDING")
	}
}

func deliveryTestTableName(model any) string {
	if model == nil {
		return ""
	}

	if _, ok := model.(*domain.Alarm); ok {
		return "alarms"
	}

	v := reflect.ValueOf(model)
	t := reflect.TypeOf(model)

	for t.Kind() == reflect.Pointer {
		if !v.IsValid() || v.IsNil() {
			t = t.Elem()
			break
		}

		v = v.Elem()
		t = t.Elem()
	}

	for t.Kind() == reflect.Slice || t.Kind() == reflect.Array {
		t = t.Elem()
		for t.Kind() == reflect.Pointer {
			t = t.Elem()
		}

		v = reflect.Zero(t)
	}

	if v.IsValid() && v.CanInterface() {
		if namer, ok := reflect.TypeAssert[interface{ TableName() string }](v); ok {
			return namer.TableName()
		}
	}

	ptr := reflect.New(t)
	if namer, ok := reflect.TypeAssert[interface{ TableName() string }](ptr); ok {
		return namer.TableName()
	}

	return deliveryTestSnakeCase(t.Name())
}

func deliveryTestColumnName(field *reflect.StructField) (string, bool) {
	if dbTag := field.Tag.Get("db"); dbTag != "" {
		name, _, _ := strings.Cut(dbTag, ",")
		return name, name != "-" && name != ""
	}

	if jsonTag := field.Tag.Get("json"); jsonTag != "" {
		name, _, _ := strings.Cut(jsonTag, ",")
		if name != "" && name != "-" {
			return name, true
		}
	}

	return deliveryTestSnakeCase(field.Name), true
}

func deliveryTestSnakeCase(name string) string {
	replacer := strings.NewReplacer(
		"ID", "Id",
		"URL", "Url",
		"HTTP", "Http",
		"JSON", "Json",
		"API", "Api",
	)

	name = replacer.Replace(name)

	var out strings.Builder

	for i, r := range name {
		if unicode.IsUpper(r) {
			if i > 0 {
				out.WriteByte('_')
			}

			out.WriteRune(unicode.ToLower(r))

			continue
		}

		out.WriteRune(r)
	}

	return out.String()
}
