package dbtest

import (
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

func TestYouTubeRequestSnapshotDropKeepsUnfinishedLegacyRows(t *testing.T) {
	pool := NewPool(t)
	ctx := t.Context()

	dir, err := resolveMigrationsDir()
	require.NoError(t, err)

	const file = "256_drop_youtube_delivery_request_snapshot_allowed.sql"

	// 전체 manifest를 적용한 DB에서 249 이후·256 이전 상태를 재현한다.
	_, err = pool.Exec(ctx, `ALTER TABLE youtube_notification_delivery ADD COLUMN request_snapshot_allowed BOOLEAN NOT NULL DEFAULT TRUE;
 INSERT INTO youtube_notification_outbox(kind,channel_id,content_id,payload) VALUES('NEW_VIDEO','drop-channel','drop-video','{}');
 INSERT INTO youtube_notification_delivery(outbox_id,room_id,request_snapshot_allowed) SELECT id,'legacy-pending',FALSE FROM youtube_notification_outbox WHERE content_id='drop-video';`)
	require.NoError(t, err)

	require.ErrorContains(t, applyMigrationFile(ctx, pool, dir, file), "column kept")
	requireRequestSnapshotColumn(t, pool, true)

	_, err = pool.Exec(ctx, `UPDATE youtube_notification_delivery SET status='SENT' WHERE room_id='legacy-pending'`)
	require.NoError(t, err)
	require.NoError(t, applyMigrationFile(ctx, pool, dir, file))
	requireRequestSnapshotColumn(t, pool, false)

	// 부분 적용 뒤 재실행처럼 열이 이미 없으면 변경 없이 끝난다.
	require.NoError(t, applyMigrationFile(ctx, pool, dir, file))
	requireRequestSnapshotColumn(t, pool, false)
}

func requireRequestSnapshotColumn(t *testing.T, pool *pgxpool.Pool, want bool) {
	t.Helper()

	var exists bool

	require.NoError(t, pool.QueryRow(t.Context(), `SELECT EXISTS (
 SELECT 1 FROM information_schema.columns
 WHERE table_schema='public' AND table_name='youtube_notification_delivery' AND column_name='request_snapshot_allowed')`).Scan(&exists))
	require.Equal(t, want, exists)
}
