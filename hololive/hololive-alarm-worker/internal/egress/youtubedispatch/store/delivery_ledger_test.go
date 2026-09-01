package store

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
	ytcontentid "github.com/kapu/hololive-shared/pkg/service/youtube/contentid"
)

func TestLedgerRecordSentPromotesQuarantineAndPreservesEarliestEvidence(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	key := ytcontentid.LogicalKey{
		Kind:      domain.OutboxKindNewVideo,
		LogicalID: "video-ledger-monotonic",
		RoomID:    "room-ledger-monotonic",
	}
	firstObservedAt := time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC)
	laterObservedAt := firstObservedAt.Add(2 * time.Minute)

	require.NoError(t, RecordDeliveryLedgerWrites(ctx, pool, LedgerStatusQuarantined, []LedgerWrite{{
		Key: key, ObservedAt: laterObservedAt, SourceDeliveryID: 20,
	}}))
	require.NoError(t, RecordDeliveryLedgerWrites(ctx, pool, LedgerStatusQuarantined, []LedgerWrite{{
		Key: key, ObservedAt: firstObservedAt, SourceDeliveryID: 10,
	}}))
	require.NoError(t, RecordDeliveryLedgerWrites(ctx, pool, LedgerStatusSent, []LedgerWrite{{
		Key: key, ObservedAt: firstObservedAt.Add(time.Minute), SourceDeliveryID: 30,
	}}))
	require.NoError(t, RecordDeliveryLedgerWrites(ctx, pool, LedgerStatusQuarantined, []LedgerWrite{{
		Key: key, ObservedAt: laterObservedAt.Add(time.Minute), SourceDeliveryID: 40,
	}}))

	record := readDeliveryLedgerRecord(t, pool, key)
	require.Equal(t, LedgerStatusSent, record.Status)
	require.Equal(t, firstObservedAt, record.FirstRecordedAt.UTC())
	require.Equal(t, laterObservedAt, record.UpdatedAt.UTC())
	require.NotNil(t, record.SentAt)
	require.Equal(t, firstObservedAt.Add(time.Minute), record.SentAt.UTC())
	require.NotNil(t, record.QuarantinedAt)
	require.Equal(t, firstObservedAt, record.QuarantinedAt.UTC())
	require.NotNil(t, record.SourceDeliveryID)
	require.Equal(t, int64(30), *record.SourceDeliveryID)
}

func readDeliveryLedgerRecord(t *testing.T, pool *pgxpool.Pool, key ytcontentid.LogicalKey) DeliveryLedgerRecord {
	t.Helper()

	var record DeliveryLedgerRecord

	require.NoError(t, pool.QueryRow(t.Context(), `
		SELECT kind, logical_id, room_id, status, first_recorded_at, updated_at,
		       sent_at, quarantined_at, source_delivery_id
		FROM youtube_notification_delivery_ledger
		WHERE kind = $1 AND logical_id = $2 AND room_id = $3
	`, key.Kind, key.LogicalID, key.RoomID).Scan(
		&record.Kind,
		&record.LogicalID,
		&record.RoomID,
		&record.Status,
		&record.FirstRecordedAt,
		&record.UpdatedAt,
		&record.SentAt,
		&record.QuarantinedAt,
		&record.SourceDeliveryID,
	))

	return record
}
