package store

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
	ytcontentid "github.com/kapu/hololive-shared/pkg/service/youtube/contentid"
)

const ledgerTestRoom = "ledger-test-room"

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

func TestLedgerRepeatedEvidenceDoesNotRewriteRow(t *testing.T) {
	pool := dbtest.NewPool(t)
	ctx := t.Context()

	for _, status := range []LedgerStatus{LedgerStatusSent, LedgerStatusQuarantined} {
		t.Run(string(status), func(t *testing.T) {
			write := LedgerWrite{
				Key:        ytcontentid.LogicalKey{Kind: domain.OutboxKindNewVideo, LogicalID: "repeat-" + string(status), RoomID: ledgerTestRoom},
				ObservedAt: time.Date(2026, time.October, 5, 0, 0, 0, 0, time.UTC), SourceDeliveryID: 10,
			}
			require.NoError(t, RecordDeliveryLedgerWrites(ctx, pool, status, []LedgerWrite{write}))

			before := ledgerRowVersion(ctx, t, pool, write.Key)
			require.NoError(t, RecordDeliveryLedgerWrites(ctx, pool, status, []LedgerWrite{write}))
			require.Equal(t, before, ledgerRowVersion(ctx, t, pool, write.Key), "identical evidence must not create a row version")

			if status == LedgerStatusSent {
				write.ObservedAt = write.ObservedAt.Add(time.Hour)
				write.SourceDeliveryID++
				require.NoError(t, RecordDeliveryLedgerWrites(ctx, pool, LedgerStatusQuarantined, []LedgerWrite{write}))
				require.Equal(t, before, ledgerRowVersion(ctx, t, pool, write.Key), "late quarantine must not rewrite SENT")
			}
		})
	}
}

func ledgerRowVersion(ctx context.Context, t *testing.T, pool *pgxpool.Pool, key ytcontentid.LogicalKey) string {
	t.Helper()

	var version string

	require.NoError(t, pool.QueryRow(ctx, `SELECT xmin::text FROM youtube_notification_delivery_ledger WHERE kind=$1 AND logical_id=$2 AND room_id=$3`, key.Kind, key.LogicalID, key.RoomID).Scan(&version))

	return version
}

func TestLedgerSentMergesEarlierAndLaterEvidence(t *testing.T) {
	pool := dbtest.NewPool(t)
	ctx := t.Context()
	start := time.Date(2026, time.October, 5, 0, 0, 0, 0, time.UTC)
	write := LedgerWrite{Key: ytcontentid.LogicalKey{Kind: domain.OutboxKindNewVideo, LogicalID: "sent-evidence", RoomID: ledgerTestRoom}}

	for _, minute := range []int64{10, 5, 15} {
		write.ObservedAt = start.Add(time.Duration(minute) * time.Minute)
		write.SourceDeliveryID = minute
		require.NoError(t, RecordDeliveryLedgerWrites(ctx, pool, LedgerStatusSent, []LedgerWrite{write}))
	}

	record := readDeliveryLedgerRecord(t, pool, write.Key)
	require.Equal(t, start.Add(5*time.Minute), record.FirstRecordedAt.UTC())
	require.NotNil(t, record.SentAt)
	require.Equal(t, start.Add(5*time.Minute), record.SentAt.UTC())
	require.Equal(t, start.Add(15*time.Minute), record.UpdatedAt.UTC())
	require.NotNil(t, record.SourceDeliveryID)
	require.EqualValues(t, 5, *record.SourceDeliveryID)

	before := ledgerRowVersion(ctx, t, pool, write.Key)

	write.ObservedAt = start.Add(10 * time.Minute)
	write.SourceDeliveryID = 99
	require.NoError(t, RecordDeliveryLedgerWrites(ctx, pool, LedgerStatusSent, []LedgerWrite{write}))
	require.Equal(t, before, ledgerRowVersion(ctx, t, pool, write.Key), "interior evidence does not change earliest sender")

	_, err := pool.Exec(ctx, `UPDATE youtube_notification_delivery_ledger SET source_delivery_id=NULL WHERE logical_id='sent-evidence'`)
	require.NoError(t, err)
	require.NoError(t, RecordDeliveryLedgerWrites(ctx, pool, LedgerStatusSent, []LedgerWrite{write}))

	record = readDeliveryLedgerRecord(t, pool, write.Key)
	require.NotNil(t, record.SourceDeliveryID)
	require.EqualValues(t, 99, *record.SourceDeliveryID)
}
