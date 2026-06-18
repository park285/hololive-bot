package dispatch_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/georgysavva/scany/v2/pgxscan"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/internal/delivery/deliverysql"
)

type deliveryTestSQLResult struct {
	Error        error
	RowsAffected int64
}

type deliveryTestDB = pgxpool.Pool

func newDeliveryIntegrationPool(t testing.TB) *pgxpool.Pool {
	t.Helper()
	return dbtest.NewPool(t)
}

func insertDeliveryTestRows(pool *pgxpool.Pool, value any) deliveryTestSQLResult {
	rows, err := insertDeliveryIntegrationRows(context.Background(), pool, value)
	return deliveryTestSQLResult{Error: err, RowsAffected: rows}
}

func firstDeliveryTestRow(pool *pgxpool.Pool, dest any, conds ...any) deliveryTestSQLResult {
	err := firstDeliveryIntegrationRow(context.Background(), pool, dest, conds...)
	return deliveryTestSQLResult{Error: err}
}

func findDeliveryTestRowsOrderedWhere(pool *pgxpool.Pool, dest any, order, where string, args ...any) deliveryTestSQLResult {
	err := findDeliveryIntegrationRows(context.Background(), pool, dest, where, order, args...)
	return deliveryTestSQLResult{Error: err}
}

func countDeliveryTestRowsWhere(pool *pgxpool.Pool, model any, dest *int64, where string, args ...any) deliveryTestSQLResult {
	table := deliveryIntegrationTableForModel(model)
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

func execDeliveryTestSQL(t testing.TB, pool *pgxpool.Pool, query string, args ...any) {
	t.Helper()
	tag, err := pool.Exec(context.Background(), deliverysql.PostgresPlaceholders(query), args...)
	require.NoError(t, err)
	require.GreaterOrEqual(t, tag.RowsAffected(), int64(0))
}

func deleteDeliveryTestRowsWhere(pool *pgxpool.Pool, model any, where string, args ...any) deliveryTestSQLResult {
	table := deliveryIntegrationTableForModel(model)
	if table == "" {
		return deliveryTestSQLResult{Error: fmt.Errorf("delete rows: unsupported model %T", model)}
	}
	query := "DELETE FROM " + table
	if strings.TrimSpace(where) != "" {
		query += " WHERE " + where
	}
	tag, err := pool.Exec(context.Background(), deliverysql.PostgresPlaceholders(query), args...)
	return deliveryTestSQLResult{Error: err, RowsAffected: tag.RowsAffected()}
}

func deleteDeliveryTestRows(t testing.TB, pool *pgxpool.Pool, model any) {
	t.Helper()
	id, ok := deliveryIntegrationID(model)
	if !ok {
		require.Failf(t, "delete delivery test rows", "unsupported model or missing id %T", model)
		return
	}
	result := deleteDeliveryTestRowsWhere(pool, model, "id = ?", id)
	require.NoError(t, result.Error)
}

func firstDeliveryIntegrationRow(ctx context.Context, pool *pgxpool.Pool, dest any, conds ...any) error {
	table := deliveryIntegrationTableForDest(dest)
	if table == "" {
		return fmt.Errorf("first row: unsupported dest %T", dest)
	}
	query := "SELECT " + deliveryIntegrationSelectColumns(table) + " FROM " + table
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
	return pgxscan.Get(ctx, pool, dest, deliverysql.PostgresPlaceholders(query), args...)
}

func findDeliveryIntegrationRows(ctx context.Context, pool *pgxpool.Pool, dest any, where, order string, args ...any) error {
	table := deliveryIntegrationTableForDest(dest)
	if table == "" {
		return fmt.Errorf("find rows: unsupported dest %T", dest)
	}
	query := "SELECT " + deliveryIntegrationSelectColumns(table) + " FROM " + table
	if strings.TrimSpace(where) != "" {
		query += " WHERE " + where
	}
	if strings.TrimSpace(order) != "" {
		query += " ORDER BY " + order
	}
	return pgxscan.Select(ctx, pool, dest, deliverysql.PostgresPlaceholders(query), args...)
}

func insertDeliveryIntegrationRows(ctx context.Context, pool *pgxpool.Pool, value any) (int64, error) {
	switch row := value.(type) {
	case *domain.YouTubeNotificationOutbox:
		return insertDeliveryIntegrationOutbox(ctx, pool, row)
	case domain.YouTubeNotificationOutbox:
		v := row
		return insertDeliveryIntegrationOutbox(ctx, pool, &v)
	case []domain.YouTubeNotificationOutbox:
		var affected int64
		for i := range row {
			n, err := insertDeliveryIntegrationOutbox(ctx, pool, &row[i])
			if err != nil {
				return affected, err
			}
			affected += n
		}
		return affected, nil
	case *[]domain.YouTubeNotificationOutbox:
		return insertDeliveryIntegrationRows(ctx, pool, *row)
	case *domain.YouTubeNotificationDelivery:
		return insertDeliveryIntegrationDelivery(ctx, pool, row)
	case domain.YouTubeNotificationDelivery:
		v := row
		return insertDeliveryIntegrationDelivery(ctx, pool, &v)
	case []domain.YouTubeNotificationDelivery:
		var affected int64
		for i := range row {
			n, err := insertDeliveryIntegrationDelivery(ctx, pool, &row[i])
			if err != nil {
				return affected, err
			}
			affected += n
		}
		return affected, nil
	case *[]domain.YouTubeNotificationDelivery:
		return insertDeliveryIntegrationRows(ctx, pool, *row)
	case *domain.YouTubeCommunityShortsAlarmState:
		return insertDeliveryIntegrationAlarmState(ctx, pool, row)
	case domain.YouTubeCommunityShortsAlarmState:
		v := row
		return insertDeliveryIntegrationAlarmState(ctx, pool, &v)
	case []domain.YouTubeCommunityShortsAlarmState:
		var affected int64
		for i := range row {
			n, err := insertDeliveryIntegrationAlarmState(ctx, pool, &row[i])
			if err != nil {
				return affected, err
			}
			affected += n
		}
		return affected, nil
	case *[]domain.YouTubeCommunityShortsAlarmState:
		return insertDeliveryIntegrationRows(ctx, pool, *row)
	default:
		return 0, fmt.Errorf("unsupported create value: %T", value)
	}
}

func deliveryIntegrationNormalizeTimePtr(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	normalized := value.UTC()
	return &normalized
}

func deliveryIntegrationRequiredTime(value, fallback time.Time) time.Time {
	if value.IsZero() {
		return fallback
	}
	return value.UTC()
}

func insertDeliveryIntegrationOutbox(ctx context.Context, pool *pgxpool.Pool, row *domain.YouTubeNotificationOutbox) (int64, error) {
	if row == nil {
		return 0, nil
	}
	now := time.Now().UTC()
	row.CreatedAt = deliveryIntegrationRequiredTime(row.CreatedAt, now)
	row.NextAttemptAt = deliveryIntegrationRequiredTime(row.NextAttemptAt, now)
	row.LockedAt = deliveryIntegrationNormalizeTimePtr(row.LockedAt)
	row.SentAt = deliveryIntegrationNormalizeTimePtr(row.SentAt)
	if row.Status == "" {
		row.Status = domain.OutboxStatusPending
	}
	err := pool.QueryRow(ctx, `INSERT INTO youtube_notification_outbox (kind, channel_id, content_id, payload, status, attempt_count, next_attempt_at, created_at, locked_at, sent_at, error) VALUES ($1, $2, $3, $4::jsonb, $5, $6, $7, $8, $9, $10, $11) RETURNING id`, row.Kind, row.ChannelID, row.ContentID, row.Payload, row.Status, row.AttemptCount, row.NextAttemptAt, row.CreatedAt, row.LockedAt, row.SentAt, row.Error).Scan(&row.ID)
	if err != nil {
		return 0, err
	}
	return 1, nil
}

func insertDeliveryIntegrationDelivery(ctx context.Context, pool *pgxpool.Pool, row *domain.YouTubeNotificationDelivery) (int64, error) {
	if row == nil {
		return 0, nil
	}
	now := time.Now().UTC()
	row.CreatedAt = deliveryIntegrationRequiredTime(row.CreatedAt, now)
	row.NextAttemptAt = deliveryIntegrationRequiredTime(row.NextAttemptAt, now)
	row.LockedAt = deliveryIntegrationNormalizeTimePtr(row.LockedAt)
	row.SentAt = deliveryIntegrationNormalizeTimePtr(row.SentAt)
	if row.Status == "" {
		row.Status = domain.OutboxStatusPending
	}
	err := pool.QueryRow(ctx, `INSERT INTO youtube_notification_delivery (outbox_id, room_id, status, attempt_count, next_attempt_at, created_at, locked_at, sent_at, error) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) RETURNING id`, row.OutboxID, row.RoomID, row.Status, row.AttemptCount, row.NextAttemptAt, row.CreatedAt, row.LockedAt, row.SentAt, row.Error).Scan(&row.ID)
	if err != nil {
		return 0, err
	}
	return 1, nil
}

func insertDeliveryIntegrationAlarmState(ctx context.Context, pool *pgxpool.Pool, row *domain.YouTubeCommunityShortsAlarmState) (int64, error) {
	if row == nil {
		return 0, nil
	}
	now := time.Now().UTC()
	row.CreatedAt = deliveryIntegrationRequiredTime(row.CreatedAt, now)
	row.UpdatedAt = deliveryIntegrationRequiredTime(row.UpdatedAt, now)
	row.DetectedAt = deliveryIntegrationRequiredTime(row.DetectedAt, now)
	row.ActualPublishedAt = deliveryIntegrationNormalizeTimePtr(row.ActualPublishedAt)
	row.PublishedAtRetryAfter = deliveryIntegrationNormalizeTimePtr(row.PublishedAtRetryAfter)
	row.AuthorizedAt = deliveryIntegrationNormalizeTimePtr(row.AuthorizedAt)
	row.AlarmSentAt = deliveryIntegrationNormalizeTimePtr(row.AlarmSentAt)
	if row.DeliveryStatus == "" {
		row.DeliveryStatus = domain.ResolveYouTubeCommunityShortsAlarmStateStatus(row.AuthorizedAt, row.AlarmSentAt)
	}
	tag, err := pool.Exec(ctx, `INSERT INTO youtube_community_shorts_alarm_states (kind, post_id, content_id, channel_id, actual_published_at, detected_at, published_at_retry_after, authorized_at, alarm_sent_at, delivery_status, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`, row.Kind, row.PostID, row.ContentID, row.ChannelID, row.ActualPublishedAt, row.DetectedAt, row.PublishedAtRetryAfter, row.AuthorizedAt, row.AlarmSentAt, row.DeliveryStatus, row.CreatedAt, row.UpdatedAt)
	return tag.RowsAffected(), err
}

func deliveryIntegrationID(model any) (int64, bool) {
	switch row := model.(type) {
	case *domain.YouTubeNotificationOutbox:
		return row.ID, row.ID != 0
	case *domain.YouTubeNotificationDelivery:
		return row.ID, row.ID != 0
	default:
		return 0, false
	}
}

func deliveryIntegrationTableForDest(dest any) string {
	switch dest.(type) {
	case *domain.YouTubeNotificationOutbox, *[]domain.YouTubeNotificationOutbox:
		return "youtube_notification_outbox"
	case *domain.YouTubeNotificationDelivery, *[]domain.YouTubeNotificationDelivery:
		return "youtube_notification_delivery"
	case *domain.YouTubeCommunityShortsAlarmState, *[]domain.YouTubeCommunityShortsAlarmState:
		return "youtube_community_shorts_alarm_states"
	default:
		return ""
	}
}

func deliveryIntegrationTableForModel(model any) string {
	switch model.(type) {
	case *domain.YouTubeNotificationOutbox, domain.YouTubeNotificationOutbox:
		return "youtube_notification_outbox"
	case *domain.YouTubeNotificationDelivery, domain.YouTubeNotificationDelivery:
		return "youtube_notification_delivery"
	case *domain.YouTubeCommunityShortsAlarmState, domain.YouTubeCommunityShortsAlarmState:
		return "youtube_community_shorts_alarm_states"
	default:
		return deliveryIntegrationTableForDest(model)
	}
}

func deliveryIntegrationSelectColumns(table string) string {
	switch table {
	case "youtube_notification_outbox":
		return "id, kind, channel_id, content_id, payload::text AS payload, status, attempt_count, next_attempt_at, created_at, locked_at, sent_at, COALESCE(error, '') AS error"
	case "youtube_notification_delivery":
		return "id, outbox_id, room_id, status, attempt_count, next_attempt_at, created_at, locked_at, sent_at, COALESCE(error, '') AS error"
	case "youtube_community_shorts_alarm_states":
		return "kind, post_id, content_id, channel_id, actual_published_at, detected_at, published_at_retry_after, authorized_at, alarm_sent_at, delivery_status, created_at, updated_at"
	default:
		return "*"
	}
}
