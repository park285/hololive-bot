package batchrepo

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/georgysavva/scany/v2/pgxscan"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/internal/dbx"
	"github.com/kapu/hololive-shared/pkg/dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
)

type batchTestDB struct {
	*pgxpool.Pool

	model any
	where string
	args  []any
	order string

	Error        error
	RowsAffected int64
}

func newBatchTestDB(t *testing.T, models ...any) *batchTestDB {
	t.Helper()

	pool := dbtest.NewPool(t)
	db := &batchTestDB{Pool: pool}
	db.resetOptionalTables(t, models...)
	return db
}

func (db *batchTestDB) batchPool() batchTxBeginner {
	return db.Pool
}

func (db *batchTestDB) resetOptionalTables(t *testing.T, models ...any) {
	t.Helper()

	keep := map[string]bool{
		"youtube_content_alarm_tracking":               true,
		"youtube_community_shorts_source_posts":        true,
		"youtube_community_shorts_alarm_states":        true,
		"youtube_notification_delivery":                true,
		"youtube_notification_delivery_telemetry":      true,
		"youtube_community_shorts_observation_windows": true,
	}
	for _, model := range models {
		keep[tableName(model)] = true
	}

	for _, table := range []string{
		"youtube_videos",
		"youtube_community_posts",
		"youtube_notification_outbox",
		"youtube_content_watermarks",
	} {
		if keep[table] {
			continue
		}
		_, err := db.Pool.Exec(context.Background(), "DROP TABLE IF EXISTS "+table+" CASCADE")
		require.NoError(t, err)
	}
}

func persistVideos(repository BatchRepository, ctx context.Context, videos []*domain.YouTubeVideo, notifications []*domain.YouTubeNotificationOutbox, watermark *domain.YouTubeContentWatermark) error {
	return repository.PersistVideos(ctx, videos, notifications, nil, watermark)
}

func persistCommunityPosts(repository BatchRepository, ctx context.Context, posts []*domain.YouTubeCommunityPost, notifications []*domain.YouTubeNotificationOutbox, watermark *domain.YouTubeContentWatermark) error {
	return repository.PersistCommunityPosts(ctx, posts, notifications, nil, watermark)
}

func (db *batchTestDB) clone() *batchTestDB {
	return &batchTestDB{Pool: db.Pool, model: db.model, where: db.where, args: append([]any(nil), db.args...), order: db.order}
}

func (db *batchTestDB) Model(model any) *batchTestDB {
	next := db.clone()
	next.model = model
	return next
}

func (db *batchTestDB) Where(query string, args ...any) *batchTestDB {
	next := db.clone()
	next.where = query
	next.args = append([]any(nil), args...)
	return next
}

func (db *batchTestDB) Order(order string) *batchTestDB {
	next := db.clone()
	next.order = order
	return next
}

func (db *batchTestDB) Count(dest *int64) *batchTestDB {
	next := db.clone()
	table := tableName(next.model)
	query := "SELECT COUNT(*) FROM " + table
	args := next.args
	if strings.TrimSpace(next.where) != "" {
		query += " WHERE " + next.where
	}
	next.Error = next.QueryRow(context.Background(), dbx.PostgresPlaceholders(query), args...).Scan(dest)
	return next
}

func (db *batchTestDB) First(dest any, conds ...any) *batchTestDB {
	next := db.clone()
	table := tableName(dest)
	query := "SELECT " + selectColumns(table) + " FROM " + table
	args := next.args
	if len(conds) > 0 {
		condition, ok := conds[0].(string)
		if !ok {
			next.Error = fmt.Errorf("first condition has type %T, want string", conds[0])
			return next
		}
		query += " WHERE " + condition
		args = append([]any(nil), conds[1:]...)
	} else if strings.TrimSpace(next.where) != "" {
		query += " WHERE " + next.where
	}
	query += " LIMIT 1"
	next.Error = pgxscan.Get(context.Background(), next.Pool, dest, dbx.PostgresPlaceholders(query), args...)
	return next
}

func (db *batchTestDB) Find(dest any) *batchTestDB {
	next := db.clone()
	table := tableName(dest)
	query := "SELECT " + selectColumns(table) + " FROM " + table
	args := next.args
	if strings.TrimSpace(next.where) != "" {
		query += " WHERE " + next.where
	}
	if strings.TrimSpace(next.order) != "" {
		query += " ORDER BY " + next.order
	}
	next.Error = pgxscan.Select(context.Background(), next.Pool, dest, dbx.PostgresPlaceholders(query), args...)
	return next
}

func (db *batchTestDB) Create(value any) *batchTestDB {
	next := db.clone()
	next.RowsAffected, next.Error = insertBatchTestValue(context.Background(), next.Pool, value)
	return next
}

func (db *batchTestDB) Exec(query string, args ...any) *batchTestDB {
	next := db.clone()
	if strings.HasPrefix(strings.ToUpper(strings.TrimSpace(query)), "PRAGMA ") {
		return next
	}
	tag, err := next.Pool.Exec(context.Background(), dbx.PostgresPlaceholders(query), args...)
	next.Error = err
	next.RowsAffected = tag.RowsAffected()
	return next
}

func insertBatchTestValue(ctx context.Context, db *pgxpool.Pool, value any) (int64, error) {
	v := reflect.ValueOf(value)
	if isReflectPointer(v) {
		v = v.Elem()
	}
	if isReflectSlice(v) {
		return insertBatchTestSlice(ctx, db, v)
	}

	switch row := value.(type) {
	case *domain.YouTubeNotificationOutbox:
		return insertOutbox(ctx, db, row)
	case *domain.YouTubeNotificationDelivery:
		return execInsert(ctx, db, `
			INSERT INTO youtube_notification_delivery
				(outbox_id, room_id, status, attempt_count, next_attempt_at, created_at, locked_at, sent_at, error)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			row.OutboxID, row.RoomID, row.Status, row.AttemptCount, row.NextAttemptAt, row.CreatedAt, row.LockedAt, row.SentAt, row.Error)
	case *domain.YouTubeContentAlarmTracking:
		if row.DeliveryStatus == "" {
			row.DeliveryStatus = domain.YouTubeContentAlarmDeliveryStatusPending
		}
		now := time.Now()
		if row.CreatedAt.IsZero() {
			row.CreatedAt = now
		}
		if row.UpdatedAt.IsZero() {
			row.UpdatedAt = now
		}
		return execInsert(ctx, db, `
			INSERT INTO youtube_content_alarm_tracking
				(kind, content_id, canonical_content_id, channel_id, actual_published_at, detected_at, alarm_sent_at, alarm_latency_millis, alarm_latency_exceeded, delivery_status, latency_classification_status, delay_source, internal_delay_cause, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			row.Kind, row.ContentID, row.CanonicalContentID, row.ChannelID, row.ActualPublishedAt, row.DetectedAt, row.AlarmSentAt, row.AlarmLatencyMillis, row.AlarmLatencyExceeded, row.DeliveryStatus, row.LatencyClassificationStatus, row.DelaySource, row.InternalDelayCause, row.CreatedAt, row.UpdatedAt)
	case *domain.YouTubeCommunityShortsAlarmState:
		if row.DeliveryStatus == "" {
			row.DeliveryStatus = domain.YouTubeCommunityShortsAlarmStateStatusDetected
		}
		now := time.Now()
		if row.CreatedAt.IsZero() {
			row.CreatedAt = now
		}
		if row.UpdatedAt.IsZero() {
			row.UpdatedAt = now
		}
		return execInsert(ctx, db, `
			INSERT INTO youtube_community_shorts_alarm_states
				(kind, post_id, content_id, channel_id, actual_published_at, detected_at, authorized_at, alarm_sent_at, delivery_status, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			row.Kind, row.PostID, row.ContentID, row.ChannelID, row.ActualPublishedAt, row.DetectedAt, row.AuthorizedAt, row.AlarmSentAt, row.DeliveryStatus, row.CreatedAt, row.UpdatedAt)
	default:
		return 0, fmt.Errorf("unsupported create value: %T", value)
	}
}

func insertBatchTestSlice(ctx context.Context, db *pgxpool.Pool, values reflect.Value) (int64, error) {
	var rows int64
	for i := 0; i < values.Len(); i++ {
		affected, err := insertBatchTestValue(ctx, db, values.Index(i).Addr().Interface())
		if err != nil {
			return rows, err
		}
		rows += affected
	}
	return rows, nil
}

func isReflectPointer(value reflect.Value) bool {
	return value.IsValid() && value.Kind() == reflect.Pointer
}

func isReflectSlice(value reflect.Value) bool {
	return value.IsValid() && value.Kind() == reflect.Slice
}

func insertOutbox(ctx context.Context, db *pgxpool.Pool, row *domain.YouTubeNotificationOutbox) (int64, error) {
	if row.Status == "" {
		row.Status = domain.OutboxStatusPending
	}
	now := time.Now()
	if row.NextAttemptAt.IsZero() {
		row.NextAttemptAt = now
	}
	if row.CreatedAt.IsZero() {
		row.CreatedAt = now
	}
	err := db.QueryRow(ctx, dbx.PostgresPlaceholders(`
		INSERT INTO youtube_notification_outbox
			(kind, channel_id, content_id, payload, status, attempt_count, next_attempt_at, created_at, locked_at, sent_at, error)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		RETURNING id`),
		row.Kind, row.ChannelID, row.ContentID, row.Payload, row.Status, row.AttemptCount, row.NextAttemptAt, row.CreatedAt, row.LockedAt, row.SentAt, row.Error,
	).Scan(&row.ID)
	if err != nil {
		return 0, err
	}
	return 1, nil
}

func execInsert(ctx context.Context, db *pgxpool.Pool, query string, args ...any) (int64, error) {
	tag, err := db.Exec(ctx, dbx.PostgresPlaceholders(query), args...)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func tableName(model any) string {
	switch model.(type) {
	case *domain.YouTubeVideo, domain.YouTubeVideo, []domain.YouTubeVideo, *[]domain.YouTubeVideo:
		return "youtube_videos"
	case *domain.YouTubeCommunityPost, domain.YouTubeCommunityPost, []domain.YouTubeCommunityPost, *[]domain.YouTubeCommunityPost:
		return "youtube_community_posts"
	case *domain.YouTubeNotificationOutbox, domain.YouTubeNotificationOutbox, []domain.YouTubeNotificationOutbox, *[]domain.YouTubeNotificationOutbox:
		return "youtube_notification_outbox"
	case *domain.YouTubeContentWatermark, domain.YouTubeContentWatermark, []domain.YouTubeContentWatermark, *[]domain.YouTubeContentWatermark:
		return "youtube_content_watermarks"
	case *domain.YouTubeContentAlarmTracking, domain.YouTubeContentAlarmTracking, []domain.YouTubeContentAlarmTracking, *[]domain.YouTubeContentAlarmTracking:
		return "youtube_content_alarm_tracking"
	case *domain.YouTubeCommunityShortsSourcePost, domain.YouTubeCommunityShortsSourcePost, []domain.YouTubeCommunityShortsSourcePost, *[]domain.YouTubeCommunityShortsSourcePost:
		return "youtube_community_shorts_source_posts"
	case *domain.YouTubeCommunityShortsAlarmState, domain.YouTubeCommunityShortsAlarmState, []domain.YouTubeCommunityShortsAlarmState, *[]domain.YouTubeCommunityShortsAlarmState:
		return "youtube_community_shorts_alarm_states"
	case *domain.YouTubeNotificationDelivery, domain.YouTubeNotificationDelivery, []domain.YouTubeNotificationDelivery, *[]domain.YouTubeNotificationDelivery:
		return "youtube_notification_delivery"
	case *domain.YouTubeNotificationDeliveryTelemetry, domain.YouTubeNotificationDeliveryTelemetry, []domain.YouTubeNotificationDeliveryTelemetry, *[]domain.YouTubeNotificationDeliveryTelemetry:
		return "youtube_notification_delivery_telemetry"
	default:
		return ""
	}
}

func selectColumns(table string) string {
	switch table {
	case "youtube_notification_outbox":
		return "id, kind, channel_id, content_id, payload, status, attempt_count, next_attempt_at, created_at, locked_at, sent_at, COALESCE(error, '') AS error"
	case "youtube_notification_delivery":
		return "id, outbox_id, room_id, status, attempt_count, next_attempt_at, created_at, locked_at, sent_at, COALESCE(error, '') AS error"
	case "youtube_content_alarm_tracking":
		return "kind, content_id, canonical_content_id, channel_id, actual_published_at, detected_at, alarm_sent_at, alarm_latency_millis, alarm_latency_exceeded, delivery_status, COALESCE(latency_classification_status, '') AS latency_classification_status, COALESCE(delay_source, '') AS delay_source, COALESCE(internal_delay_cause, '') AS internal_delay_cause, created_at, updated_at"
	default:
		return "*"
	}
}
