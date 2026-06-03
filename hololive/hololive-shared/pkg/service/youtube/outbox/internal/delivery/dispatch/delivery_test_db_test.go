package dispatch

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
	"unicode"

	"github.com/georgysavva/scany/v2/pgxscan"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/internal/delivery/deliverysql"
)

type deliveryTestDB struct {
	*pgxpool.Pool

	model any
	where string
	args  []any
	order string

	Error        error
	RowsAffected int64
}

func newDeliveryTestDB(t *testing.T) *deliveryTestDB {
	t.Helper()
	pool := dbtest.NewPool(t)
	for _, statement := range []string{
		`ALTER TABLE youtube_notification_delivery DROP CONSTRAINT IF EXISTS youtube_notification_delivery_outbox_id_fkey`,
		`ALTER TABLE youtube_notification_delivery_telemetry DROP CONSTRAINT IF EXISTS youtube_notification_delivery_telemetry_outbox_id_fkey`,
		`ALTER TABLE youtube_community_shorts_observation_post_baselines DROP CONSTRAINT IF EXISTS fk_ycsopb_observation_window`,
	} {
		if _, err := pool.Exec(context.Background(), statement); err != nil {
			t.Fatalf("delivery test db: relax legacy unit-test constraint: %v", err)
		}
	}
	return &deliveryTestDB{Pool: pool}
}

func newDeliveryExecModePool(t *testing.T, db *deliveryTestDB) *pgxpool.Pool {
	t.Helper()
	cfg := db.Pool.Config()
	cfg.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeExec
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return pool
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
	table := deliveryTestTableName(next.model)
	query := "SELECT COUNT(*) FROM " + table
	args := next.args
	if strings.TrimSpace(next.where) != "" {
		query += " WHERE " + next.where
	}
	next.Error = next.QueryRow(context.Background(), deliverysql.PostgresPlaceholders(query), args...).Scan(dest)
	return next
}

func (db *deliveryTestDB) First(dest any, conds ...any) *deliveryTestDB {
	next := db.clone()
	table := deliveryTestTableName(dest)
	query := "SELECT " + deliveryTestSelectColumns(table) + " FROM " + table
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
	next.Error = pgxscan.Get(context.Background(), next.Pool, dest, deliverysql.PostgresPlaceholders(query), args...)
	return next
}

func (db *deliveryTestDB) Find(dest any) *deliveryTestDB {
	next := db.clone()
	table := deliveryTestTableName(dest)
	query := "SELECT " + deliveryTestSelectColumns(table) + " FROM " + table
	args := next.args
	if strings.TrimSpace(next.where) != "" {
		query += " WHERE " + next.where
	}
	if strings.TrimSpace(next.order) != "" {
		query += " ORDER BY " + next.order
	}
	next.Error = pgxscan.Select(context.Background(), next.Pool, dest, deliverysql.PostgresPlaceholders(query), args...)
	return next
}

func (db *deliveryTestDB) Create(value any) *deliveryTestDB {
	next := db.clone()
	next.RowsAffected, next.Error = deliveryTestInsertValue(context.Background(), next.Pool, value)
	return next
}

func (db *deliveryTestDB) Updates(values map[string]any) *deliveryTestDB {
	next := db.clone()
	table := deliveryTestTableName(next.model)
	if table == "" {
		next.Error = fmt.Errorf("updates without model")
		return next
	}
	if len(values) == 0 {
		return next
	}

	assignments := make([]string, 0, len(values))
	args := make([]any, 0, len(values)+len(next.args))
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		args = append(args, values[key])
		assignments = append(assignments, deliveryTestUpdateAssignment(key))
	}

	query := "UPDATE " + table + " SET " + strings.Join(assignments, ", ")
	if strings.TrimSpace(next.where) != "" {
		query += " WHERE " + next.where
	}
	args = append(args, next.args...)

	tag, err := next.Pool.Exec(context.Background(), deliverysql.PostgresPlaceholders(query), args...)
	next.Error = err
	next.RowsAffected = tag.RowsAffected()
	return next
}

func deliveryTestUpdateAssignment(column string) string {
	switch column {
	case "actual_published_at",
		"alarm_sent_at",
		"attempt_finished_at",
		"attempt_started_at",
		"authorized_at",
		"created_at",
		"detected_at",
		"event_at",
		"locked_at",
		"logged_at",
		"next_attempt_at",
		"observation_bigbang_cutover_at",
		"observation_ended_at",
		"observation_started_at",
		"published_at_retry_after",
		"sent_at",
		"updated_at":
		return fmt.Sprintf("%s = ?::timestamptz", column)
	default:
		return fmt.Sprintf("%s = ?", column)
	}
}

func (db *deliveryTestDB) Exec(query string, args ...any) *deliveryTestDB {
	next := db.clone()
	if strings.HasPrefix(strings.ToUpper(strings.TrimSpace(query)), "PRAGMA ") {
		return next
	}
	tag, err := next.Pool.Exec(context.Background(), deliverysql.PostgresPlaceholders(query), args...)
	next.Error = err
	next.RowsAffected = tag.RowsAffected()
	return next
}

func deliveryTestInsertValue(ctx context.Context, db *pgxpool.Pool, value any) (int64, error) {
	v := reflect.ValueOf(value)
	if !v.IsValid() {
		return 0, nil
	}
	if v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return 0, nil
		}
		v = v.Elem()
	}
	if v.Kind() == reflect.Slice {
		var rows int64
		for i := 0; i < v.Len(); i++ {
			item := v.Index(i)
			if item.Kind() == reflect.Pointer {
				affected, err := deliveryTestInsertValue(ctx, db, item.Interface())
				if err != nil {
					return rows, err
				}
				rows += affected
				continue
			}
			affected, err := deliveryTestInsertValue(ctx, db, item.Addr().Interface())
			if err != nil {
				return rows, err
			}
			rows += affected
		}
		return rows, nil
	}
	if v.Kind() != reflect.Struct {
		return 0, fmt.Errorf("unsupported create value: %T", value)
	}

	if alarm, ok := value.(*domain.Alarm); ok {
		return deliveryTestInsertAlarm(ctx, db, alarm)
	}

	deliveryTestApplyCreateDefaults(v)
	table := deliveryTestTableName(value)
	if table == "" {
		return 0, fmt.Errorf("unsupported create table for %T", value)
	}

	columns := make([]string, 0, v.NumField())
	placeholders := make([]string, 0, v.NumField())
	args := make([]any, 0, v.NumField())
	var idField reflect.Value
	omitID := false

	for i := 0; i < v.NumField(); i++ {
		field := v.Type().Field(i)
		valueField := v.Field(i)
		if field.PkgPath != "" || field.Anonymous {
			continue
		}
		column, ok := deliveryTestColumnName(field)
		if !ok {
			continue
		}
		if strings.EqualFold(column, "id") && deliveryTestIsZero(valueField) {
			idField = valueField
			omitID = true
			continue
		}
		columns = append(columns, column)
		placeholders = append(placeholders, fmt.Sprintf("$%d", len(args)+1))
		args = append(args, valueField.Interface())
	}
	if len(columns) == 0 {
		return 0, fmt.Errorf("no insert columns for %T", value)
	}

	query := "INSERT INTO " + table + " (" + strings.Join(columns, ", ") + ") VALUES (" + strings.Join(placeholders, ", ") + ")"
	if omitID && idField.IsValid() && idField.CanSet() {
		query += " RETURNING id"
		if err := db.QueryRow(ctx, query, args...).Scan(idField.Addr().Interface()); err != nil {
			return 0, err
		}
		return 1, nil
	}
	tag, err := db.Exec(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func deliveryTestInsertAlarm(ctx context.Context, db *pgxpool.Pool, alarm *domain.Alarm) (int64, error) {
	if alarm.CreatedAt.IsZero() {
		alarm.CreatedAt = time.Now().UTC()
	}
	alarmTypes, err := alarm.AlarmTypes.Value()
	if err != nil {
		return 0, err
	}
	err = db.QueryRow(ctx, `
		INSERT INTO alarms (room_id, user_id, channel_id, member_name, room_name, user_name, alarm_types, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7::alarm_type[], $8)
		RETURNING id
	`, alarm.RoomID, alarm.UserID, alarm.ChannelID, alarm.MemberName, alarm.RoomName, alarm.UserName, alarmTypes, alarm.CreatedAt).Scan(&alarm.ID)
	if err != nil {
		return 0, err
	}
	return 1, nil
}

func deliveryTestApplyCreateDefaults(v reflect.Value) {
	now := time.Now().UTC()
	timeType := reflect.TypeFor[time.Time]()
	for i := 0; i < v.NumField(); i++ {
		field := v.Type().Field(i)
		value := v.Field(i)
		if field.PkgPath != "" || !value.CanSet() {
			continue
		}
		if value.Type() == timeType {
			if t, ok := value.Interface().(time.Time); ok && !t.IsZero() {
				value.Set(reflect.ValueOf(t.UTC()))
			}
		}
		if value.Kind() == reflect.Pointer && value.Type().Elem() == timeType && !value.IsNil() {
			t := value.Elem().Interface().(time.Time).UTC()
			value.Set(reflect.ValueOf(&t))
		}
		if field.Name == "CreatedAt" || field.Name == "UpdatedAt" {
			if t, ok := value.Interface().(time.Time); ok && t.IsZero() {
				value.Set(reflect.ValueOf(now))
			}
		}
		if field.Name == "NextAttemptAt" {
			if t, ok := value.Interface().(time.Time); ok && t.IsZero() {
				value.Set(reflect.ValueOf(now))
			}
		}
	}
	contentID := v.FieldByName("ContentID")
	canonicalContentID := v.FieldByName("CanonicalContentID")
	if contentID.IsValid() && canonicalContentID.IsValid() &&
		contentID.Kind() == reflect.String && canonicalContentID.Kind() == reflect.String &&
		canonicalContentID.CanSet() && canonicalContentID.String() == "" {
		canonicalContentID.SetString(contentID.String())
	}
	deliveryStatus := v.FieldByName("DeliveryStatus")
	if deliveryStatus.IsValid() && deliveryStatus.CanSet() && deliveryStatus.Kind() == reflect.String && deliveryStatus.String() == "" {
		deliveryStatus.SetString("PENDING")
	}
}

func deliveryTestIsZero(v reflect.Value) bool {
	return v.IsZero()
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
		if namer, ok := v.Interface().(interface{ TableName() string }); ok {
			return namer.TableName()
		}
	}
	ptr := reflect.New(t)
	if namer, ok := ptr.Interface().(interface{ TableName() string }); ok {
		return namer.TableName()
	}
	return deliveryTestSnakeCase(t.Name())
}

func deliveryTestColumnName(field reflect.StructField) (string, bool) {
	if dbTag := field.Tag.Get("db"); dbTag != "" {
		name := strings.Split(dbTag, ",")[0]
		return name, name != "-" && name != ""
	}
	if jsonTag := field.Tag.Get("json"); jsonTag != "" {
		name := strings.Split(jsonTag, ",")[0]
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

func deliveryTestSelectColumns(table string) string {
	switch table {
	case "youtube_notification_outbox":
		return "id, kind, channel_id, content_id, payload::text AS payload, status, attempt_count, next_attempt_at, created_at, locked_at, sent_at, COALESCE(error, '') AS error"
	case "youtube_notification_delivery":
		return "id, outbox_id, room_id, status, attempt_count, next_attempt_at, created_at, locked_at, sent_at, COALESCE(error, '') AS error"
	case "youtube_content_alarm_tracking":
		return "kind, content_id, COALESCE(canonical_content_id, '') AS canonical_content_id, channel_id, actual_published_at, detected_at, alarm_sent_at, alarm_latency_millis, alarm_latency_exceeded, COALESCE(delivery_status, '') AS delivery_status, COALESCE(latency_classification_status, '') AS latency_classification_status, COALESCE(delay_source, '') AS delay_source, COALESCE(internal_delay_cause, '') AS internal_delay_cause, created_at, updated_at"
	case "youtube_community_shorts_alarm_states":
		return "kind, post_id, content_id, channel_id, actual_published_at, detected_at, published_at_retry_after, authorized_at, alarm_sent_at, delivery_status, created_at, updated_at"
	case "youtube_notification_delivery_telemetry":
		return "id, delivery_id, attempt_ordinal, outbox_id, channel_id, content_id, COALESCE(post_id, '') AS post_id, room_id, alarm_type, actual_published_at, alarm_sent_at, alarm_latency_millis, detected_at, COALESCE(observation_status, '') AS observation_status, COALESCE(observation_runtime_name, '') AS observation_runtime_name, observation_bigbang_cutover_at, observation_started_at, observation_ended_at, COALESCE(dedupe_key, '') AS dedupe_key, COALESCE(delivery_path, '') AS delivery_path, COALESCE(delivery_mode, '') AS delivery_mode, COALESCE(send_result, '') AS send_result, COALESCE(failure_reason, '') AS failure_reason, attempt_started_at, attempt_finished_at, event_at, next_attempt_at, created_at, locked_at, logged_at, COALESCE(error, '') AS error"
	default:
		return "*"
	}
}
