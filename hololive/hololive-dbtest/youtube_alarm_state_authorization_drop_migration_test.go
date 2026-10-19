package dbtest

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

const (
	alarmStateAuthorizationDropMigration = "331_drop_youtube_alarm_state_authorization.sql"
	alarmStateDeliveryStatusVocab        = "chk_youtube_community_shorts_alarm_states_delivery_status_vocab"
)

type alarmStateRow struct {
	status      string
	alarmSentAt *time.Time
	updatedAt   time.Time
}

func TestYouTubeAlarmStateAuthorizationDropNormalizesLegacyClaims(t *testing.T) {
	pool := NewPool(t)
	ctx := t.Context()

	dir, err := resolveMigrationsDir()
	require.NoError(t, err)

	restorePreAuthorizationDropSchema(t, pool)

	seededAt := time.Date(2026, time.April, 10, 1, 0, 0, 0, time.UTC)
	authorizedAt := seededAt.Add(30 * time.Second)
	sentAt := seededAt.Add(time.Minute)

	// 구 워커가 남긴 선점(미발송·발송 시각 있음), 3c-3 upsert가 DETECTED로 다시 계산한 선점, 정상 발송·감지 행.
	_, err = pool.Exec(ctx, `
		INSERT INTO youtube_community_shorts_alarm_states
			(kind, post_id, content_id, channel_id, detected_at, authorized_at, alarm_sent_at, delivery_status, created_at, updated_at)
		VALUES
			('COMMUNITY_POST', 'community:claimed', 'claimed', 'UC_alarm_state', $1, $2, NULL, 'ENQUEUED', $1, $1),
			('NEW_SHORT', 'short:claimed-sent', 'short:claimed-sent', 'UC_alarm_state', $1, $2, $3, 'ENQUEUED', $1, $1),
			('COMMUNITY_POST', 'community:reupserted', 'reupserted', 'UC_alarm_state', $1, $2, NULL, 'DETECTED', $1, $1),
			('COMMUNITY_POST', 'community:sent', 'sent', 'UC_alarm_state', $1, NULL, $3, 'SENT', $1, $1),
			('NEW_SHORT', 'short:detected', 'short:detected', 'UC_alarm_state', $1, NULL, NULL, 'DETECTED', $1, $1)`,
		seededAt, authorizedAt, sentAt)
	require.NoError(t, err)

	require.NoError(t, applyMigrationFile(ctx, pool, dir, alarmStateAuthorizationDropMigration))

	applied := alarmStateRows(t, pool)
	require.Len(t, applied, 5)

	// 남은 ENQUEUED만 alarm_sent_at으로 다시 계산하고, 나머지 행은 updated_at까지 그대로 둔다.
	for postID, want := range map[string]struct {
		status      string
		alarmSentAt *time.Time
		normalized  bool
	}{
		"community:claimed":    {status: "DETECTED", normalized: true},
		"short:claimed-sent":   {status: "SENT", alarmSentAt: new(sentAt), normalized: true},
		"community:reupserted": {status: "DETECTED"},
		"community:sent":       {status: "SENT", alarmSentAt: new(sentAt)},
		"short:detected":       {status: "DETECTED"},
	} {
		got := applied[postID]
		require.Equal(t, want.status, got.status, postID)
		require.Equal(t, want.alarmSentAt == nil, got.alarmSentAt == nil, postID)

		if want.alarmSentAt != nil {
			require.Equal(t, *want.alarmSentAt, got.alarmSentAt.UTC(), postID)
		}

		if want.normalized {
			require.True(t, got.updatedAt.After(seededAt), postID)
		} else {
			require.Equal(t, seededAt, got.updatedAt.UTC(), postID)
		}
	}

	requireAuthorizationDropped(t, pool)

	// 파일을 다시 적용해도 행과 스키마가 그대로다.
	require.NoError(t, applyMigrationFile(ctx, pool, dir, alarmStateAuthorizationDropMigration))
	require.Equal(t, applied, alarmStateRows(t, pool))
	requireAuthorizationDropped(t, pool)

	_, err = pool.Exec(ctx, `UPDATE youtube_community_shorts_alarm_states SET delivery_status = 'ENQUEUED' WHERE post_id = 'short:detected'`)
	requireSQLState(t, err, sqlStateCheckViolation)
}

// restorePreAuthorizationDropSchema는 전체 manifest를 적용한 DB를 331 이전 열·색인·CHECK로 되돌린다.
func restorePreAuthorizationDropSchema(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	_, err := pool.Exec(t.Context(), `
		ALTER TABLE youtube_community_shorts_alarm_states ADD COLUMN authorized_at timestamptz;
		CREATE INDEX idx_ycsas_authorized_at ON youtube_community_shorts_alarm_states (authorized_at DESC) WHERE authorized_at IS NOT NULL;
		ALTER TABLE youtube_community_shorts_alarm_states DROP CONSTRAINT `+alarmStateDeliveryStatusVocab+`;
		ALTER TABLE youtube_community_shorts_alarm_states ADD CONSTRAINT `+alarmStateDeliveryStatusVocab+`
			CHECK (delivery_status IN ('DETECTED', 'ENQUEUED', 'SENT'));`)
	require.NoError(t, err)
}

func alarmStateRows(t *testing.T, pool *pgxpool.Pool) map[string]alarmStateRow {
	t.Helper()

	rows, err := pool.Query(t.Context(), `SELECT post_id, delivery_status, alarm_sent_at, updated_at FROM youtube_community_shorts_alarm_states`)
	require.NoError(t, err)

	defer rows.Close()

	got := make(map[string]alarmStateRow)

	for rows.Next() {
		var (
			postID string
			row    alarmStateRow
		)

		require.NoError(t, rows.Scan(&postID, &row.status, &row.alarmSentAt, &row.updatedAt))

		got[postID] = row
	}

	require.NoError(t, rows.Err())

	return got
}

func requireAuthorizationDropped(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	var columnExists, indexExists bool

	require.NoError(t, pool.QueryRow(t.Context(), `SELECT
		EXISTS (SELECT 1 FROM information_schema.columns
			WHERE table_schema = 'public' AND table_name = 'youtube_community_shorts_alarm_states' AND column_name = 'authorized_at'),
		to_regclass('public.idx_ycsas_authorized_at') IS NOT NULL`).Scan(&columnExists, &indexExists))
	require.False(t, columnExists)
	require.False(t, indexExists)
	require.True(t, constraintValidated(t, pool, "youtube_community_shorts_alarm_states", alarmStateDeliveryStatusVocab))
}
