package polling

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/georgysavva/scany/v2/pgxscan"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

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

	db := &batchTestDB{Pool: dbtest.NewPool(t)}
	db.relaxLegacyFixtureConstraints(t)
	db.resetOptionalTables(t, models...)
	return db
}

func (db *batchTestDB) relaxLegacyFixtureConstraints(t *testing.T) {
	t.Helper()

	_, err := db.Pool.Exec(context.Background(), "ALTER TABLE youtube_videos ALTER COLUMN video_id TYPE text")
	require.NoError(t, err)
}

func (db *batchTestDB) clone() *batchTestDB {
	return &batchTestDB{
		Pool:  db.Pool,
		model: db.model,
		where: db.where,
		args:  append([]any(nil), db.args...),
		order: db.order,
	}
}

func (db *batchTestDB) resetOptionalTables(t *testing.T, models ...any) {
	t.Helper()

	keep := map[string]bool{
		"youtube_content_alarm_tracking":          true,
		"youtube_community_shorts_source_posts":   true,
		"youtube_community_shorts_alarm_states":   true,
		"youtube_notification_delivery":           true,
		"youtube_notification_delivery_telemetry": true,
	}
	for _, model := range models {
		keep[publishedAtResolverTestTableName(model)] = true
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
	table := publishedAtResolverTestTableName(next.model)
	query := "SELECT COUNT(*) FROM " + table
	args := next.args
	if strings.TrimSpace(next.where) != "" {
		query += " WHERE " + next.where
	}
	next.Error = next.QueryRow(context.Background(), publishedAtResolverTestPlaceholders(query), args...).Scan(dest)
	return next
}

func (db *batchTestDB) First(dest any, conds ...any) *batchTestDB {
	next := db.clone()
	table := publishedAtResolverTestTableName(dest)
	query := "SELECT " + publishedAtResolverTestSelectColumns(table) + " FROM " + table
	args := next.args
	if len(conds) > 0 {
		query += " WHERE " + conds[0].(string)
		args = append([]any(nil), conds[1:]...)
	} else if strings.TrimSpace(next.where) != "" {
		query += " WHERE " + next.where
	}
	query += " LIMIT 1"
	next.Error = pgxscan.Get(context.Background(), next.Pool, dest, publishedAtResolverTestPlaceholders(query), args...)
	return next
}

func (db *batchTestDB) Find(dest any) *batchTestDB {
	next := db.clone()
	table := publishedAtResolverTestTableName(dest)
	query := "SELECT " + publishedAtResolverTestSelectColumns(table) + " FROM " + table
	args := next.args
	if strings.TrimSpace(next.where) != "" {
		query += " WHERE " + next.where
	}
	if strings.TrimSpace(next.order) != "" {
		query += " ORDER BY " + next.order
	}
	next.Error = pgxscan.Select(context.Background(), next.Pool, dest, publishedAtResolverTestPlaceholders(query), args...)
	return next
}

func (db *batchTestDB) Create(value any) *batchTestDB {
	next := db.clone()
	next.RowsAffected, next.Error = insertPublishedAtResolverTestValue(context.Background(), next.Pool, value)
	return next
}

func (db *batchTestDB) ExecTest(query string, args ...any) *batchTestDB {
	next := db.clone()
	next.RowsAffected, next.Error = execPublishedAtResolverTestSQL(context.Background(), next.Pool, query, args...)
	return next
}

func (db *batchTestDB) Delete(value any, query string, args ...any) *batchTestDB {
	next := db.clone()
	table := publishedAtResolverTestTableName(value)
	tag, err := next.Pool.Exec(context.Background(), publishedAtResolverTestPlaceholders("DELETE FROM "+table+" WHERE "+query), args...)
	next.Error = err
	next.RowsAffected = tag.RowsAffected()
	return next
}

func (db *batchTestDB) Update(column string, value any) *batchTestDB {
	return db.Updates(map[string]any{column: value})
}

func (db *batchTestDB) Updates(values map[string]any) *batchTestDB {
	next := db.clone()
	table := publishedAtResolverTestTableName(next.model)
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	sets := make([]string, 0, len(keys))
	args := make([]any, 0, len(keys)+len(next.args))
	for _, key := range keys {
		sets = append(sets, key+" = ?")
		args = append(args, values[key])
	}
	query := "UPDATE " + table + " SET " + strings.Join(sets, ", ")
	if strings.TrimSpace(next.where) != "" {
		query += " WHERE " + next.where
		args = append(args, next.args...)
	}

	tag, err := next.Pool.Exec(context.Background(), publishedAtResolverTestPlaceholders(query), args...)
	next.Error = err
	next.RowsAffected = tag.RowsAffected()
	return next
}

func insertPublishedAtResolverTestValue(ctx context.Context, db *pgxpool.Pool, value any) (int64, error) {
	v := reflect.ValueOf(value)
	if v.Kind() == reflect.Pointer {
		v = v.Elem()
	}
	if v.Kind() == reflect.Slice {
		var rows int64
		for i := 0; i < v.Len(); i++ {
			affected, err := insertPublishedAtResolverTestValue(ctx, db, v.Index(i).Addr().Interface())
			if err != nil {
				return rows, err
			}
			rows += affected
		}
		return rows, nil
	}

	switch row := value.(type) {
	case *domain.YouTubeVideo:
		return insertPublishedAtResolverTestVideo(ctx, db, row)
	case *domain.YouTubeCommunityPost:
		return insertPublishedAtResolverTestCommunityPost(ctx, db, row)
	case *domain.YouTubeNotificationOutbox:
		return insertPublishedAtResolverTestOutbox(ctx, db, row)
	case *domain.YouTubeContentAlarmTracking:
		return insertPublishedAtResolverTestTracking(ctx, db, row)
	case *domain.YouTubeCommunityShortsSourcePost:
		return insertPublishedAtResolverTestSourcePost(ctx, db, row)
	case *domain.YouTubeCommunityShortsAlarmState:
		return insertPublishedAtResolverTestAlarmState(ctx, db, row)
	default:
		return 0, fmt.Errorf("unsupported create value: %T", value)
	}
}

func insertPublishedAtResolverTestVideo(ctx context.Context, db *pgxpool.Pool, row *domain.YouTubeVideo) (int64, error) {
	now := time.Now().UTC()
	if row.FirstSeenAt.IsZero() {
		row.FirstSeenAt = now
	}
	if row.LastSeenAt.IsZero() {
		row.LastSeenAt = now
	}
	return execPublishedAtResolverTestInsert(ctx, db, `
		INSERT INTO youtube_videos
			(video_id, channel_id, title, thumbnail, duration, published_text, published_at, is_short, is_live_replay, view_count, first_seen_at, last_seen_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		row.VideoID, row.ChannelID, row.Title, row.Thumbnail, row.Duration, row.PublishedText, row.PublishedAt, row.IsShort, row.IsLiveReplay, row.ViewCount, row.FirstSeenAt, row.LastSeenAt)
}

func insertPublishedAtResolverTestCommunityPost(ctx context.Context, db *pgxpool.Pool, row *domain.YouTubeCommunityPost) (int64, error) {
	now := time.Now().UTC()
	if row.FirstSeenAt.IsZero() {
		row.FirstSeenAt = now
	}
	if row.LastSeenAt.IsZero() {
		row.LastSeenAt = now
	}
	return execPublishedAtResolverTestInsert(ctx, db, `
		INSERT INTO youtube_community_posts
			(post_id, channel_id, author_name, author_photo, content_text, published_text, published_at, like_count, comment_count, images, attached_video, first_seen_at, last_seen_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		row.PostID, row.ChannelID, row.AuthorName, row.AuthorPhoto, row.ContentText, row.PublishedText, row.PublishedAt, row.LikeCount, row.CommentCount, row.Images, row.AttachedVideo, row.FirstSeenAt, row.LastSeenAt)
}

func insertPublishedAtResolverTestOutbox(ctx context.Context, db *pgxpool.Pool, row *domain.YouTubeNotificationOutbox) (int64, error) {
	if row.Status == "" {
		row.Status = domain.OutboxStatusPending
	}
	now := time.Now().UTC()
	if row.NextAttemptAt.IsZero() {
		row.NextAttemptAt = now
	}
	if row.CreatedAt.IsZero() {
		row.CreatedAt = now
	}
	err := db.QueryRow(ctx, publishedAtResolverTestPlaceholders(`
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

func insertPublishedAtResolverTestTracking(ctx context.Context, db *pgxpool.Pool, row *domain.YouTubeContentAlarmTracking) (int64, error) {
	if row.DeliveryStatus == "" {
		row.DeliveryStatus = domain.YouTubeContentAlarmDeliveryStatusPending
	}
	if row.CanonicalContentID == "" {
		row.CanonicalContentID = row.ContentID
	}
	now := time.Now().UTC()
	if row.CreatedAt.IsZero() {
		row.CreatedAt = now
	}
	if row.UpdatedAt.IsZero() {
		row.UpdatedAt = now
	}
	return execPublishedAtResolverTestInsert(ctx, db, `
		INSERT INTO youtube_content_alarm_tracking
			(kind, content_id, canonical_content_id, channel_id, actual_published_at, detected_at, alarm_sent_at, alarm_latency_millis, alarm_latency_exceeded, delivery_status, latency_classification_status, delay_source, internal_delay_cause, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		row.Kind, row.ContentID, row.CanonicalContentID, row.ChannelID, row.ActualPublishedAt, row.DetectedAt, row.AlarmSentAt, row.AlarmLatencyMillis, row.AlarmLatencyExceeded, row.DeliveryStatus, row.LatencyClassificationStatus, row.DelaySource, row.InternalDelayCause, row.CreatedAt, row.UpdatedAt)
}

func insertPublishedAtResolverTestSourcePost(ctx context.Context, db *pgxpool.Pool, row *domain.YouTubeCommunityShortsSourcePost) (int64, error) {
	now := time.Now().UTC()
	if row.CreatedAt.IsZero() {
		row.CreatedAt = now
	}
	if row.UpdatedAt.IsZero() {
		row.UpdatedAt = now
	}
	return execPublishedAtResolverTestInsert(ctx, db, `
		INSERT INTO youtube_community_shorts_source_posts
			(kind, post_id, channel_id, actual_published_at, detected_at, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		row.Kind, row.PostID, row.ChannelID, row.ActualPublishedAt, row.DetectedAt, row.CreatedAt, row.UpdatedAt)
}

func insertPublishedAtResolverTestAlarmState(ctx context.Context, db *pgxpool.Pool, row *domain.YouTubeCommunityShortsAlarmState) (int64, error) {
	if row.DeliveryStatus == "" {
		row.DeliveryStatus = domain.YouTubeCommunityShortsAlarmStateStatusDetected
	}
	now := time.Now().UTC()
	if row.CreatedAt.IsZero() {
		row.CreatedAt = now
	}
	if row.UpdatedAt.IsZero() {
		row.UpdatedAt = now
	}
	return execPublishedAtResolverTestInsert(ctx, db, `
		INSERT INTO youtube_community_shorts_alarm_states
			(kind, post_id, content_id, channel_id, actual_published_at, detected_at, published_at_retry_after, authorized_at, alarm_sent_at, delivery_status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		row.Kind, row.PostID, row.ContentID, row.ChannelID, row.ActualPublishedAt, row.DetectedAt, row.PublishedAtRetryAfter, row.AuthorizedAt, row.AlarmSentAt, row.DeliveryStatus, row.CreatedAt, row.UpdatedAt)
}

func execPublishedAtResolverTestInsert(ctx context.Context, db *pgxpool.Pool, query string, args ...any) (int64, error) {
	tag, err := db.Exec(ctx, publishedAtResolverTestPlaceholders(query), args...)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func execPublishedAtResolverTestSQL(ctx context.Context, db *pgxpool.Pool, query string, args ...any) (int64, error) {
	trimmed := strings.TrimSpace(query)
	if strings.HasPrefix(strings.ToUpper(trimmed), "PRAGMA ") {
		return 0, nil
	}
	if handled, affected, err := execPublishedAtResolverTestTrigger(ctx, db, trimmed); handled {
		return affected, err
	}
	tag, err := db.Exec(ctx, publishedAtResolverTestPlaceholders(query), args...)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func execPublishedAtResolverTestTrigger(ctx context.Context, db *pgxpool.Pool, query string) (bool, int64, error) {
	upper := strings.ToUpper(query)
	switch {
	case strings.Contains(upper, "CREATE TRIGGER FAIL_OUTBOX_INSERT_KEEP_RETRY_AFTER"):
		return true, 0, createPublishedAtResolverTestTrigger(ctx, db, "fail_outbox_insert_keep_retry_after", "youtube_notification_outbox", "BEFORE INSERT", "outbox blocked")
	case strings.Contains(upper, "CREATE TRIGGER FAIL_OUTBOX_INSERT_FOR_RETRY_AFTER"):
		return true, 0, createPublishedAtResolverTestTrigger(ctx, db, "fail_outbox_insert_for_retry_after", "youtube_notification_outbox", "BEFORE INSERT", "outbox blocked")
	case strings.Contains(upper, "CREATE TRIGGER FAIL_OUTBOX_INSERT"):
		return true, 0, createPublishedAtResolverTestTrigger(ctx, db, "fail_outbox_insert", "youtube_notification_outbox", "BEFORE INSERT", "outbox blocked")
	case strings.Contains(upper, "CREATE TRIGGER FAIL_RETRY_AFTER_UPDATE"):
		return true, 0, createPublishedAtResolverTestTrigger(ctx, db, "fail_retry_after_update", "youtube_community_shorts_alarm_states", "BEFORE UPDATE OF published_at_retry_after", "retry_after blocked")
	default:
		return false, 0, nil
	}
}

func createPublishedAtResolverTestTrigger(ctx context.Context, db *pgxpool.Pool, name string, table string, timing string, message string) error {
	functionName := name + "_fn"
	if _, err := db.Exec(ctx, fmt.Sprintf(`
		CREATE OR REPLACE FUNCTION %s()
		RETURNS trigger
		LANGUAGE plpgsql
		AS $$
		BEGIN
			RAISE EXCEPTION '%s';
		END;
		$$`, functionName, message)); err != nil {
		return err
	}
	_, err := db.Exec(ctx, fmt.Sprintf(`
		CREATE TRIGGER %s
		%s ON %s
		FOR EACH ROW
		EXECUTE FUNCTION %s()`, name, timing, table, functionName))
	return err
}

func publishedAtResolverTestTableName(model any) string {
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

func publishedAtResolverTestSelectColumns(table string) string {
	switch table {
	case "youtube_notification_outbox":
		return "id, kind, channel_id, content_id, payload::text AS payload, status, attempt_count, next_attempt_at, created_at, locked_at, sent_at, COALESCE(error, '') AS error"
	case "youtube_notification_delivery":
		return "id, outbox_id, room_id, status, attempt_count, next_attempt_at, created_at, locked_at, sent_at, COALESCE(error, '') AS error"
	case "youtube_content_alarm_tracking":
		return "kind, content_id, canonical_content_id, channel_id, actual_published_at, detected_at, alarm_sent_at, alarm_latency_millis, alarm_latency_exceeded, delivery_status, COALESCE(latency_classification_status, '') AS latency_classification_status, COALESCE(delay_source, '') AS delay_source, COALESCE(internal_delay_cause, '') AS internal_delay_cause, created_at, updated_at"
	default:
		return "*"
	}
}

func publishedAtResolverTestPlaceholders(query string) string {
	var out strings.Builder
	index := 1
	for i := 0; i < len(query); i++ {
		if query[i] != '?' {
			out.WriteByte(query[i])
			continue
		}
		out.WriteString(fmt.Sprintf("$%d", index))
		index++
	}
	return out.String()
}
