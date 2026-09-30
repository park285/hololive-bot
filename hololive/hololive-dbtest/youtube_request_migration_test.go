package dbtest

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestYouTubeRequestMigrationPreservesLegacyAndReplays(t *testing.T) {
	pool := NewPool(t)
	ctx := t.Context()
	_, err := pool.Exec(ctx, `ALTER TABLE youtube_notification_delivery DROP COLUMN send_request_id, DROP COLUMN request_snapshot_allowed;
 DROP TABLE youtube_notification_send_request;
 INSERT INTO youtube_notification_outbox(kind,channel_id,content_id,payload) VALUES('NEW_VIDEO','migration-channel','migration-video','{}');
 INSERT INTO youtube_notification_delivery(outbox_id,room_id) SELECT id,'legacy-room' FROM youtube_notification_outbox WHERE content_id='migration-video';`)
	require.NoError(t, err)

	dir, err := resolveMigrationsDir()
	require.NoError(t, err)

	const file = "249_youtube_delivery_immutable_requests.sql"

	require.NoError(t, applyMigrationFile(ctx, pool, dir, file))

	var allowed bool

	require.NoError(t, pool.QueryRow(ctx, `SELECT request_snapshot_allowed FROM youtube_notification_delivery WHERE room_id='legacy-room'`).Scan(&allowed))
	require.False(t, allowed)

	// DEFAULT 변경 직전 중단된 부분 적용 상태를 재생한다.
	_, err = pool.Exec(ctx, `ALTER TABLE youtube_notification_delivery ALTER COLUMN request_snapshot_allowed SET DEFAULT FALSE`)
	require.NoError(t, err)
	require.NoError(t, applyMigrationFile(ctx, pool, dir, file))

	_, err = pool.Exec(ctx, `INSERT INTO youtube_notification_delivery(outbox_id,room_id) SELECT id,'new-room' FROM youtube_notification_outbox WHERE content_id='migration-video'`)
	require.NoError(t, err)
	require.NoError(t, pool.QueryRow(ctx, `SELECT request_snapshot_allowed FROM youtube_notification_delivery WHERE room_id='new-room'`).Scan(&allowed))
	require.True(t, allowed)
	require.NoError(t, pool.QueryRow(ctx, `SELECT request_snapshot_allowed FROM youtube_notification_delivery WHERE room_id='legacy-room'`).Scan(&allowed))
	require.False(t, allowed)

	_, err = pool.Exec(ctx, `INSERT INTO youtube_notification_send_request(base_id,room_id,message,message_hash,route,dedupe_keys,member_ids,generation) VALUES('migration:id','new-room','body',repeat('a',64),'text',ARRAY['key'],ARRAY[1]::bigint[],2)`)
	require.NoError(t, err)
	require.NoError(t, applyMigrationFile(ctx, pool, dir, file))

	var generation int

	require.NoError(t, pool.QueryRow(ctx, `SELECT generation FROM youtube_notification_send_request WHERE base_id='migration:id'`).Scan(&generation))
	require.Equal(t, 2, generation)

	_, err = pool.Exec(ctx, `UPDATE youtube_notification_send_request SET generation=3 WHERE base_id='migration:id'`)
	require.Error(t, err)
}
