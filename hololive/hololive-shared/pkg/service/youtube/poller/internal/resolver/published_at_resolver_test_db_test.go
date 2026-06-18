package resolver

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/georgysavva/scany/v2/pgxscan"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kapu/hololive-shared/pkg/dbtest"
	"github.com/stretchr/testify/require"
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
