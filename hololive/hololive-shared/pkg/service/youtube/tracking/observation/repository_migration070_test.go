package observation

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/stretchr/testify/require"
)

const migration070Filename = "070_repoint_youtube_content_alarm_tracking_pk_to_canonical.sql"

func readMigration070SQL(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	require.NoError(t, err)
	for {
		if _, statErr := os.Stat(filepath.Join(dir, "build-all.sh")); statErr == nil {
			migrationsDir := filepath.Join(dir, "hololive", "hololive-api", "scripts", "migrations")
			data, readErr := fs.ReadFile(os.DirFS(migrationsDir), migration070Filename)
			require.NoError(t, readErr)
			return string(data)
		}
		parent := filepath.Dir(dir)
		require.NotEqual(t, parent, dir, "repo root marker build-all.sh not found above cwd")
		dir = parent
	}
}

func applyMigration070(t *testing.T, db trackingDB) {
	t.Helper()
	ctx := context.Background()
	for _, stmt := range splitMigration070Statements(readMigration070SQL(t)) {
		_, err := db.Exec(ctx, stmt)
		require.NoError(t, err)
	}
}

func splitMigration070Statements(sql string) []string {
	var (
		statements []string
		builder    strings.Builder
		inDollar   bool
	)
	for line := range strings.SplitSeq(sql, "\n") {
		if strings.Contains(line, "$$") {
			inDollar = !inDollar
		}
		builder.WriteString(line)
		builder.WriteString("\n")
		if inDollar {
			continue
		}
		if strings.HasSuffix(strings.TrimSpace(line), ";") {
			if stmt := strings.TrimSpace(builder.String()); stmt != "" {
				statements = append(statements, stmt)
			}
			builder.Reset()
		}
	}
	if stmt := strings.TrimSpace(builder.String()); stmt != "" {
		statements = append(statements, stmt)
	}
	return statements
}

func insertRawTrackingRow(t *testing.T, db trackingDB, kind domain.OutboxKind, contentID, canonicalID string, detectedAt time.Time, alarmSentAt *time.Time) {
	t.Helper()
	deliveryStatus := domain.YouTubeContentAlarmDeliveryStatusPending
	if alarmSentAt != nil {
		deliveryStatus = domain.YouTubeContentAlarmDeliveryStatusSent
	}
	_, err := db.Exec(context.Background(), `
		INSERT INTO youtube_content_alarm_tracking
			(kind, content_id, canonical_content_id, channel_id, detected_at, alarm_sent_at, delivery_status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, NOW(), NOW())
	`, kind, contentID, canonicalID, "UC_BACKFILL", detectedAt, alarmSentAt, deliveryStatus)
	require.NoError(t, err)
}

func dropCanonicalUniqueIndex(t *testing.T, db trackingDB) {
	t.Helper()
	_, err := db.Exec(context.Background(), `DROP INDEX IF EXISTS idx_ycat_kind_canonical_content`)
	require.NoError(t, err)
}

func trackingPrimaryKeyColumns(t *testing.T, db trackingDB) []string {
	t.Helper()
	rows, err := db.Query(context.Background(), `
		SELECT a.attname
		FROM pg_constraint c
		JOIN pg_class t ON t.oid = c.conrelid
		JOIN pg_attribute a ON a.attrelid = t.oid AND a.attnum = ANY(c.conkey)
		WHERE t.relname = 'youtube_content_alarm_tracking'
		  AND c.contype = 'p'
		ORDER BY array_position(c.conkey, a.attnum)
	`)
	require.NoError(t, err)
	defer rows.Close()
	var cols []string
	for rows.Next() {
		var col string
		require.NoError(t, rows.Scan(&col))
		cols = append(cols, col)
	}
	require.NoError(t, rows.Err())
	return cols
}

func TestMigration070RepointsPrimaryKeyToCanonical(t *testing.T) {
	db := newTrackingTestDB(t)
	require.Equal(t, []string{"kind", "canonical_content_id"}, trackingPrimaryKeyColumns(t, db))

	canonicalID := "community:pk-1"
	detectedAt := time.Date(2026, 4, 10, 1, 0, 0, 0, time.UTC)
	insertRawTrackingRow(t, db, domain.OutboxKindCommunityPost, "raw-a", canonicalID, detectedAt, nil)

	_, err := db.Exec(context.Background(), `
		INSERT INTO youtube_content_alarm_tracking
			(kind, content_id, canonical_content_id, channel_id, detected_at, delivery_status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, 'PENDING', NOW(), NOW())
	`, domain.OutboxKindCommunityPost, "raw-b", canonicalID, "UC_BACKFILL", detectedAt)
	require.Error(t, err)
	require.Contains(t, err.Error(), "youtube_content_alarm_tracking_pkey")
}

func TestMigration070DedupesLegacyDuplicateCanonicalRows(t *testing.T) {
	db := newTrackingTestDB(t)
	kind := domain.OutboxKindCommunityPost
	canonicalID := "community:backfill-1"
	detectedRaw := time.Date(2026, 4, 10, 1, 0, 0, 0, time.UTC)
	detectedCanonical := time.Date(2026, 4, 10, 1, 2, 0, 0, time.UTC)
	alarmSentAt := time.Date(2026, 4, 10, 1, 3, 0, 0, time.UTC)

	restoreLegacyContentIDPrimaryKey(t, db)
	insertRawTrackingRow(t, db, kind, "backfill-1", canonicalID, detectedRaw, &alarmSentAt)
	insertRawTrackingRow(t, db, kind, "backfill-2", canonicalID, detectedCanonical, nil)

	preRows := selectTrackingRowsForTest(t, db)
	require.Len(t, preRows, 2)

	applyMigration070(t, db)

	rows := selectTrackingRowsForTest(t, db)
	require.Len(t, rows, 1)
	require.Equal(t, kind, rows[0].Kind)
	require.Equal(t, canonicalID, rows[0].CanonicalContentID)
	require.Equal(t, []string{"kind", "canonical_content_id"}, trackingPrimaryKeyColumns(t, db))
}

func restoreLegacyContentIDPrimaryKey(t *testing.T, db trackingDB) {
	t.Helper()
	ctx := context.Background()
	_, err := db.Exec(ctx, `
		ALTER TABLE youtube_content_alarm_tracking
		DROP CONSTRAINT IF EXISTS youtube_content_alarm_tracking_pkey
	`)
	require.NoError(t, err)
	dropCanonicalUniqueIndex(t, db)
	_, err = db.Exec(ctx, `
		ALTER TABLE youtube_content_alarm_tracking
		ADD CONSTRAINT youtube_content_alarm_tracking_pkey PRIMARY KEY (kind, content_id)
	`)
	require.NoError(t, err)
}

func TestMigration070IsIdempotent(t *testing.T) {
	db := newTrackingTestDB(t)
	applyMigration070(t, db)
	require.Equal(t, []string{"kind", "canonical_content_id"}, trackingPrimaryKeyColumns(t, db))
	applyMigration070(t, db)
	require.Equal(t, []string{"kind", "canonical_content_id"}, trackingPrimaryKeyColumns(t, db))
}
