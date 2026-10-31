package dbtest

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAlarmRequestMigrationPreservesLegacyAndReplaysAfterPartialApply(t *testing.T) {
	t.Parallel()

	pool := NewPool(t)
	ctx := t.Context()
	_, err := pool.Exec(ctx, `ALTER TABLE alarm_dispatch_send_units DROP CONSTRAINT chk_alarm_send_request_shape,
 DROP COLUMN request_body, DROP COLUMN request_route, DROP COLUMN request_body_hash, DROP COLUMN request_delivery_ids,
 DROP COLUMN base_client_request_id, DROP COLUMN request_generation;
 INSERT INTO alarm_dispatch_send_units(unit_key,dispatch_group_key,room_id,client_request_id) VALUES(repeat('a',64),'migration-group','migration-room','migration:request');
 ALTER TABLE alarm_dispatch_send_units ADD COLUMN request_body TEXT`)
	require.NoError(t, err)

	dir, err := resolveMigrationsDir()
	require.NoError(t, err)

	const file = "248_alarm_dispatch_immutable_request.sql"

	require.NoError(t, applyMigrationFile(ctx, pool, dir, file))

	var body *string

	var id string

	var generation int

	require.NoError(t, pool.QueryRow(ctx, `SELECT request_body,client_request_id,request_generation FROM alarm_dispatch_send_units WHERE room_id='migration-room'`).Scan(&body, &id, &generation))
	require.Nil(t, body)
	require.Equal(t, "migration:request", id)
	require.Zero(t, generation)

	_, err = pool.Exec(ctx, `UPDATE alarm_dispatch_send_units SET request_body='frozen',request_route='markdown',request_body_hash=repeat('b',64),request_delivery_ids=ARRAY[1,2]::bigint[],base_client_request_id=client_request_id,client_request_id=client_request_id||':r2',request_generation=2 WHERE room_id='migration-room'`)
	require.NoError(t, err)
	require.NoError(t, applyMigrationFile(ctx, pool, dir, file))

	var pinnedBody string

	require.NoError(t, pool.QueryRow(ctx, `SELECT request_body,client_request_id,request_generation FROM alarm_dispatch_send_units WHERE room_id='migration-room'`).Scan(&pinnedBody, &id, &generation))
	require.Equal(t, "frozen", pinnedBody)
	require.Equal(t, "migration:request:r2", id)
	require.Equal(t, 2, generation)

	_, err = pool.Exec(ctx, `UPDATE alarm_dispatch_send_units SET request_generation=3 WHERE room_id='migration-room'`)
	require.Error(t, err)

	_, err = pool.Exec(ctx, `UPDATE alarm_dispatch_send_units SET request_route=NULL WHERE room_id='migration-room'`)
	require.Error(t, err)
}

func TestRichTextRequestMigrationPreservesLegacySnapshots(t *testing.T) {
	t.Parallel()

	pool := NewPool(t)
	ctx := t.Context()
	_, err := pool.Exec(ctx, `
INSERT INTO alarm_dispatch_send_units(unit_key,dispatch_group_key,room_id,client_request_id,
request_body,request_route,request_body_hash,request_delivery_ids,base_client_request_id,request_generation)
VALUES(repeat('a',64),'richtext-migration','legacy-text','legacy:text:r1',
'**원문**','text',repeat('c',64),ARRAY[1]::bigint[],'legacy:text',1),
(repeat('b',64),'richtext-migration','legacy-markdown','legacy:markdown:r2',
'**원문**','markdown',repeat('d',64),ARRAY[2]::bigint[],'legacy:markdown',2);
INSERT INTO youtube_notification_send_request(base_id,room_id,message,message_hash,route,dedupe_keys,member_ids,generation)
VALUES('legacy:text','legacy-text','**원문**',repeat('c',64),'text',ARRAY['key-text'],ARRAY[1]::bigint[],1),
('legacy:markdown','legacy-markdown','**원문**',repeat('d',64),'markdown',ARRAY['key-markdown'],ARRAY[2]::bigint[],2)`)
	require.NoError(t, err)

	const snapshot = `SELECT
(SELECT jsonb_agg(to_jsonb(u) ORDER BY id)::text FROM alarm_dispatch_send_units u),
(SELECT jsonb_agg(to_jsonb(r) ORDER BY base_id)::text FROM youtube_notification_send_request r)`

	var alarmBefore, youtubeBefore string

	require.NoError(t, pool.QueryRow(ctx, snapshot).Scan(&alarmBefore, &youtubeBefore))

	dir, err := resolveMigrationsDir()
	require.NoError(t, err)

	for range 2 {
		require.NoError(t, applyMigrationFile(ctx, pool, dir, "345_alarm_richtext_immutable_request.sql"))
		require.NoError(t, applyMigrationFile(ctx, pool, dir, "346_youtube_richtext_immutable_request.sql"))

		var alarmAfter, youtubeAfter string

		require.NoError(t, pool.QueryRow(ctx, snapshot).Scan(&alarmAfter, &youtubeAfter))
		require.Equal(t, alarmBefore, alarmAfter)
		require.Equal(t, youtubeBefore, youtubeAfter)
	}

	_, err = pool.Exec(ctx, `
INSERT INTO alarm_dispatch_send_units(unit_key,dispatch_group_key,room_id,client_request_id,
request_body,request_route,request_body_hash,request_delivery_ids,base_client_request_id)
VALUES(repeat('e',64),'richtext-migration','new-richtext','new:richtext',
'**새 본문**','richtext',repeat('f',64),ARRAY[3]::bigint[],'new:richtext');
INSERT INTO youtube_notification_send_request(base_id,room_id,message,message_hash,route,dedupe_keys,member_ids)
VALUES('new:richtext','new-richtext','**새 본문**',repeat('f',64),'richtext',ARRAY['key-richtext'],ARRAY[3]::bigint[])`)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `UPDATE alarm_dispatch_send_units SET request_route='unsupported' WHERE room_id='new-richtext'`)
	require.Error(t, err)

	_, err = pool.Exec(ctx, `UPDATE youtube_notification_send_request SET route='unsupported' WHERE base_id='new:richtext'`)
	require.Error(t, err)

	_, err = pool.Exec(ctx, `UPDATE alarm_dispatch_send_units SET request_body_hash=NULL WHERE room_id='new-richtext'`)
	require.Error(t, err)
}
