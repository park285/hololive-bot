package dbtest

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAlarmRequestMigrationPreservesLegacyAndReplaysAfterPartialApply(t *testing.T) {
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
