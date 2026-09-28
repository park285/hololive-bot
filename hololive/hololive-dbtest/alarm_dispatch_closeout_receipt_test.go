package dbtest

import (
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

const (
	closeoutReceiptID = "123e4567-e89b-42d3-a456-426614174000"
	closeoutCall      = `SELECT record_alarm_dispatch_closeout($1::uuid, $2, $3::bigint[], $4, 'reviewer', 'provider outcome reviewed; no replay')`
)

type closeoutReceiptFixture struct {
	unitID       int64
	firstID      int64
	secondID     int64
	originalRows string
	ids          []int64
	digest       string
}

// 격리 묶음의 모든 원본 행은 그대로 두고, 보존 기간 뒤에도 검증 가능한 결정 영수증만 남긴다.
func TestAlarmDispatchCloseoutReceiptPreservesOriginalAndSurvivesRetention(t *testing.T) {
	pool := NewPool(t)
	fixture := seedCloseoutReceiptFixture(t, pool)

	assertCloseoutReceiptRejectsUnreviewedSnapshot(t, pool, fixture)
	recordCloseoutReceipt(t, pool, fixture)
	assertCloseoutReceiptPreservesOriginalAndAudit(t, pool, fixture)
	assertCloseoutReceiptAppendOnlyAndRetentionUnchanged(t, pool, fixture)
}

func seedCloseoutReceiptFixture(t *testing.T, pool *pgxpool.Pool) closeoutReceiptFixture {
	t.Helper()

	var eventID int64

	fixture := closeoutReceiptFixture{}
	ctx := t.Context()

	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO alarm_dispatch_events
		    (event_key, payload_hash, alarm_type, channel_id, stream_id, category, payload)
		VALUES ('closeout-event', repeat('a', 64), 'LIVE', 'closeout-channel', 'closeout-stream',
		        'closeout', '{"message":"private-payload-marker"}'::jsonb)
		RETURNING id`).Scan(&eventID))

	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO alarm_dispatch_send_units
		    (unit_key, dispatch_group_key, room_id, client_request_id)
		VALUES (repeat('b', 64), 'closeout-group', 'closeout-room', 'closeout-request')
		RETURNING id`).Scan(&fixture.unitID))

	for index, id := range []*int64{&fixture.firstID, &fixture.secondID} {
		require.NoError(t, pool.QueryRow(ctx, `
			INSERT INTO alarm_dispatch_deliveries
			    (event_id, room_id, dedupe_key, status, quarantined_at, send_unit_id,
			     dispatch_group_key, delivery_context, last_error_code, last_error)
			VALUES ($1, 'closeout-room', $2, 'quarantined', now(), $3,
			        'closeout-group', '{"message":"private-context-marker"}'::jsonb,
			        'private code marker', 'private-error-marker')
			RETURNING id`, eventID, "closeout-dedupe-"+string(rune('a'+index)), fixture.unitID).Scan(id))
	}

	require.NoError(t, pool.QueryRow(ctx, `SELECT jsonb_agg(to_jsonb(d) ORDER BY d.id)::text
		FROM alarm_dispatch_deliveries d WHERE send_unit_id = $1`, fixture.unitID).Scan(&fixture.originalRows))

	require.NoError(t, pool.QueryRow(ctx, `SELECT target_ids, original_sha256
		FROM alarm_dispatch_closeout_snapshot($1)`, fixture.firstID).Scan(&fixture.ids, &fixture.digest))
	require.Equal(t, []int64{fixture.firstID, fixture.secondID}, fixture.ids)
	require.Len(t, fixture.digest, 64)

	return fixture
}

func assertCloseoutReceiptRejectsUnreviewedSnapshot(t *testing.T, pool *pgxpool.Pool, fixture closeoutReceiptFixture) {
	t.Helper()

	ctx := t.Context()

	_, err := pool.Exec(ctx, closeoutCall, closeoutReceiptID, fixture.firstID, fixture.ids, fixture.digest)
	require.ErrorContains(t, err, "requires serializable transaction")

	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	require.NoError(t, err)

	_, err = tx.Exec(ctx, closeoutCall, closeoutReceiptID, fixture.firstID, fixture.ids[:1], fixture.digest)
	require.ErrorContains(t, err, "snapshot changed")
	require.NoError(t, tx.Rollback(ctx))

	tx, err = pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	require.NoError(t, err)

	_, err = tx.Exec(ctx, closeoutCall, closeoutReceiptID, fixture.firstID, fixture.ids, strings.Repeat("0", 64))
	require.ErrorContains(t, err, "snapshot changed")
	require.NoError(t, tx.Rollback(ctx))
}

func recordCloseoutReceipt(t *testing.T, pool *pgxpool.Pool, fixture closeoutReceiptFixture) {
	t.Helper()

	ctx := t.Context()

	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	require.NoError(t, err)

	_, err = tx.Exec(ctx, closeoutCall, closeoutReceiptID, fixture.firstID, fixture.ids, fixture.digest)
	require.NoError(t, err)
	require.NoError(t, tx.Commit(ctx))
}

func assertCloseoutReceiptPreservesOriginalAndAudit(t *testing.T, pool *pgxpool.Pool, fixture closeoutReceiptFixture) {
	t.Helper()

	var retainedRows, receiptText string

	ctx := t.Context()

	require.NoError(t, pool.QueryRow(ctx, `SELECT jsonb_agg(to_jsonb(d) ORDER BY d.id)::text
		FROM alarm_dispatch_deliveries d WHERE send_unit_id = $1`, fixture.unitID).Scan(&retainedRows))
	require.Equal(t, fixture.originalRows, retainedRows)

	require.NoError(t, pool.QueryRow(ctx, `SELECT to_jsonb(r)::text
		FROM alarm_dispatch_closeout_receipts r WHERE receipt_id = $1::uuid`, closeoutReceiptID).Scan(&receiptText))

	for _, sensitive := range []string{"private-payload-marker", "private-context-marker", "private code marker", "private-error-marker"} {
		require.NotContains(t, receiptText, sensitive)
	}

	require.Contains(t, receiptText, fixture.digest)
}

func assertCloseoutReceiptAppendOnlyAndRetentionUnchanged(t *testing.T, pool *pgxpool.Pool, fixture closeoutReceiptFixture) {
	t.Helper()

	var receiptCount int

	ctx := t.Context()

	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	require.NoError(t, err)

	_, err = tx.Exec(ctx, closeoutCall, "123e4567-e89b-42d3-a456-426614174001", fixture.firstID, fixture.ids, fixture.digest)
	require.ErrorContains(t, err, "already has a closeout receipt")
	require.NoError(t, tx.Rollback(ctx))

	_, err = pool.Exec(ctx, `UPDATE alarm_dispatch_closeout_receipts SET reason = 'changed'`)
	require.ErrorContains(t, err, "append-only")

	_, err = pool.Exec(ctx, `DELETE FROM alarm_dispatch_closeout_receipts`)
	require.ErrorContains(t, err, "append-only")

	_, err = pool.Exec(ctx, `TRUNCATE alarm_dispatch_closeout_receipts`)
	require.ErrorContains(t, err, "append-only")

	// 기존 delivery retention 삭제를 새 영수증이 막지 않는다.
	_, err = pool.Exec(ctx, `DELETE FROM alarm_dispatch_deliveries WHERE send_unit_id = $1`, fixture.unitID)
	require.NoError(t, err)

	require.NoError(t, pool.QueryRow(ctx, `SELECT count(receipt_id) FROM alarm_dispatch_closeout_receipts
		WHERE receipt_id = $1::uuid`, closeoutReceiptID).Scan(&receiptCount))
	require.Equal(t, 1, receiptCount)
}
