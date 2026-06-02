package delivery_test

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/georgysavva/scany/v2/pgxscan"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kapu/hololive-shared/pkg/dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
)

type deliveryTestDB struct {
	Pool *pgxpool.Pool

	model any
	where string
	args  []any
	order string

	Error        error
	RowsAffected int64
}

func newDeliveryIntegrationTestDB(t testing.TB) *deliveryTestDB {
	t.Helper()
	return &deliveryTestDB{Pool: dbtest.NewPool(t)}
}

func (db *deliveryTestDB) clone() *deliveryTestDB {
	return &deliveryTestDB{
		Pool:  db.Pool,
		model: db.model,
		where: db.where,
		args:  append([]any(nil), db.args...),
		order: db.order,
	}
}

func (db *deliveryTestDB) Model(model any) *deliveryTestDB {
	next := db.clone()
	next.model = model
	return next
}

func (db *deliveryTestDB) Where(query string, args ...any) *deliveryTestDB {
	next := db.clone()
	next.where = query
	next.args = append([]any(nil), args...)
	return next
}

func (db *deliveryTestDB) Order(order string) *deliveryTestDB {
	next := db.clone()
	next.order = order
	return next
}

func (db *deliveryTestDB) Count(dest *int64) *deliveryTestDB {
	next := db.clone()
	query := "SELECT COUNT(*) FROM " + deliveryIntegrationTableName(next.model)
	args := next.args
	if strings.TrimSpace(next.where) != "" {
		query += " WHERE " + next.where
	}
	next.Error = next.Pool.QueryRow(context.Background(), deliveryIntegrationPlaceholders(query), args...).Scan(dest)
	return next
}

func (db *deliveryTestDB) First(dest any, conds ...any) *deliveryTestDB {
	next := db.clone()
	table := deliveryIntegrationTableName(dest)
	query := "SELECT " + deliveryIntegrationSelectColumns(table) + " FROM " + table
	args := next.args
	if len(conds) > 0 {
		switch cond := conds[0].(type) {
		case string:
			query += " WHERE " + cond
			args = append([]any(nil), conds[1:]...)
		default:
			query += " WHERE id = ?"
			args = []any{cond}
		}
	} else if strings.TrimSpace(next.where) != "" {
		query += " WHERE " + next.where
	}
	query += " LIMIT 1"
	next.Error = pgxscan.Get(context.Background(), next.Pool, dest, deliveryIntegrationPlaceholders(query), args...)
	return next
}

func (db *deliveryTestDB) Find(dest any) *deliveryTestDB {
	next := db.clone()
	table := deliveryIntegrationTableName(dest)
	query := "SELECT " + deliveryIntegrationSelectColumns(table) + " FROM " + table
	args := next.args
	if strings.TrimSpace(next.where) != "" {
		query += " WHERE " + next.where
	}
	if strings.TrimSpace(next.order) != "" {
		query += " ORDER BY " + next.order
	}
	next.Error = pgxscan.Select(context.Background(), next.Pool, dest, deliveryIntegrationPlaceholders(query), args...)
	return next
}

func (db *deliveryTestDB) Create(value any) *deliveryTestDB {
	next := db.clone()
	next.RowsAffected, next.Error = deliveryIntegrationInsert(context.Background(), next.Pool, value)
	return next
}

func (db *deliveryTestDB) Exec(query string, args ...any) *deliveryTestDB {
	next := db.clone()
	tag, err := next.Pool.Exec(context.Background(), deliveryIntegrationPlaceholders(query), args...)
	next.Error = err
	next.RowsAffected = tag.RowsAffected()
	return next
}

func (db *deliveryTestDB) Delete(value any) *deliveryTestDB {
	next := db.clone()
	table := deliveryIntegrationTableName(value)
	query := "DELETE FROM " + table
	args := next.args
	if strings.TrimSpace(next.where) != "" {
		query += " WHERE " + next.where
	} else {
		id, ok := deliveryIntegrationID(value)
		if !ok {
			next.Error = fmt.Errorf("delete without where or id for %T", value)
			return next
		}
		query += " WHERE id = ?"
		args = []any{id}
	}

	tag, err := next.Pool.Exec(context.Background(), deliveryIntegrationPlaceholders(query), args...)
	next.Error = err
	next.RowsAffected = tag.RowsAffected()
	return next
}

func deliveryIntegrationInsert(ctx context.Context, db *pgxpool.Pool, value any) (int64, error) {
	now := time.Now().UTC()
	switch row := value.(type) {
	case *domain.YouTubeNotificationOutbox:
		if row.CreatedAt.IsZero() {
			row.CreatedAt = now
		}
		if row.NextAttemptAt.IsZero() {
			row.NextAttemptAt = now
		}
		err := db.QueryRow(ctx, `
			INSERT INTO youtube_notification_outbox
				(kind, channel_id, content_id, payload, status, attempt_count, next_attempt_at, created_at, locked_at, sent_at, error)
			VALUES ($1, $2, $3, $4::jsonb, $5, $6, $7, $8, $9, $10, $11)
			RETURNING id
		`, row.Kind, row.ChannelID, row.ContentID, row.Payload, row.Status, row.AttemptCount, row.NextAttemptAt, row.CreatedAt, row.LockedAt, row.SentAt, row.Error).Scan(&row.ID)
		if err != nil {
			return 0, err
		}
		return 1, nil
	case *domain.YouTubeNotificationDelivery:
		if row.CreatedAt.IsZero() {
			row.CreatedAt = now
		}
		if row.NextAttemptAt.IsZero() {
			row.NextAttemptAt = now
		}
		err := db.QueryRow(ctx, `
			INSERT INTO youtube_notification_delivery
				(outbox_id, room_id, status, attempt_count, next_attempt_at, created_at, locked_at, sent_at, error)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
			RETURNING id
		`, row.OutboxID, row.RoomID, row.Status, row.AttemptCount, row.NextAttemptAt, row.CreatedAt, row.LockedAt, row.SentAt, row.Error).Scan(&row.ID)
		if err != nil {
			return 0, err
		}
		return 1, nil
	default:
		return 0, fmt.Errorf("unsupported create value: %T", value)
	}
}

func deliveryIntegrationID(model any) (int64, bool) {
	v := reflect.ValueOf(model)
	for v.IsValid() && v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return 0, false
		}
		v = v.Elem()
	}
	if !v.IsValid() || v.Kind() != reflect.Struct {
		return 0, false
	}
	field := v.FieldByName("ID")
	if !field.IsValid() || !field.CanInt() {
		return 0, false
	}
	id := field.Int()
	return id, id != 0
}

func deliveryIntegrationTableName(model any) string {
	if model == nil {
		return ""
	}
	t := reflect.TypeOf(model)
	for t.Kind() == reflect.Pointer || t.Kind() == reflect.Slice || t.Kind() == reflect.Array {
		t = t.Elem()
	}
	switch t {
	case reflect.TypeOf(domain.YouTubeNotificationOutbox{}):
		return "youtube_notification_outbox"
	case reflect.TypeOf(domain.YouTubeNotificationDelivery{}):
		return "youtube_notification_delivery"
	case reflect.TypeOf(domain.YouTubeCommunityShortsAlarmState{}):
		return "youtube_community_shorts_alarm_states"
	default:
		return ""
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

func deliveryIntegrationPlaceholders(query string) string {
	var builder strings.Builder
	arg := 1
	for _, r := range query {
		if r == '?' {
			builder.WriteString(fmt.Sprintf("$%d", arg))
			arg++
			continue
		}
		builder.WriteRune(r)
	}
	return builder.String()
}
