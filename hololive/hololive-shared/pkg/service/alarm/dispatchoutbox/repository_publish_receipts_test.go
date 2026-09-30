package dispatchoutbox

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
)

func receiptEnvelope(room, title string) domain.AlarmQueueEnvelope {
	start := time.Date(2026, time.September, 30, 12, 0, 0, 0, time.UTC)

	return domain.AlarmQueueEnvelope{Version: 1, Notification: domain.AlarmNotification{
		AlarmType: domain.AlarmTypeLive, RoomID: room, MinutesUntil: 5,
		Channel: &domain.Channel{ID: "receipt-channel"},
		Stream:  &domain.Stream{ID: "receipt-video", ChannelID: "receipt-channel", Title: title, StartScheduled: &start},
	}}
}

func TestPublishReceiptsDistinguishMixedCommittedInputs(t *testing.T) {
	pool := dbtest.NewPool(t)
	repository := NewPgxRepositoryFromPool(pool, nil)
	ctx := t.Context()
	active := receiptEnvelope("room-active", "canonical")
	sent := receiptEnvelope("room-sent", "canonical")
	terminal := receiptEnvelope("room-terminal", "canonical")
	_, err := repository.InsertBatch(ctx, PublishBatchInput{Envelopes: []domain.AlarmQueueEnvelope{active, sent, terminal}})
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `UPDATE alarm_dispatch_deliveries SET status='sent', sent_at=NOW() WHERE room_id=$1`, sent.Notification.RoomID)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `UPDATE alarm_dispatch_deliveries SET status='quarantined', quarantined_at=NOW() WHERE room_id=$1`, terminal.Notification.RoomID)
	require.NoError(t, err)

	batch := []domain.AlarmQueueEnvelope{receiptEnvelope("room-collision", "changed"), active, receiptEnvelope("room-new", "canonical"), sent, terminal}
	result, err := repository.InsertBatch(ctx, PublishBatchInput{Envelopes: batch})
	require.NoError(t, err)
	require.Len(t, result.Receipts, len(batch))

	want := []PublishOutcome{PublishRejectedCollision, PublishDuplicateActive, PublishInserted, PublishDuplicateSent, PublishRejectedTerminal}

	for i, receipt := range result.Receipts {
		require.Equal(t, i, receipt.Ordinal)
		require.Equal(t, BuildDedupeKeyFromEnvelope(&batch[i]), receipt.DedupeKey)
		require.Equal(t, want[i], receipt.Outcome)
	}

	require.Equal(t, 1, result.InsertedDeliveries)
	require.Equal(t, 3, result.DuplicateDeliveries)
	require.Equal(t, 2, result.TerminalDuplicates)

	var count int

	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM alarm_dispatch_deliveries WHERE room_id='room-collision'`).Scan(&count))
	require.Zero(t, count)
}

func TestPublishCommitReconciliationVerifiesLedgerAndLeavesMissingUnresolved(t *testing.T) {
	pool := dbtest.NewPool(t)
	repository := NewPgxRepositoryFromPool(pool, nil)
	ctx := t.Context()
	committed := receiptEnvelope("room-committed", "canonical")
	_, err := repository.InsertBatch(ctx, PublishBatchInput{Envelopes: []domain.AlarmQueueEnvelope{committed}})
	require.NoError(t, err)

	lost := errors.New("commit response lost")
	result, err := repository.reconcilePublishCommit(ctx, []domain.AlarmQueueEnvelope{receiptEnvelope("room-missing", "canonical"), committed}, lost)
	require.ErrorIs(t, err, lost)
	require.Len(t, result.Receipts, 1)
	require.Equal(t, 1, result.Receipts[0].Ordinal)
	require.Equal(t, PublishDuplicateActive, result.Receipts[0].Outcome)
}

func TestPublishRolledBackChunkHasNoAcceptedReceipts(t *testing.T) {
	pool := dbtest.NewPool(t)
	repository := NewPgxRepositoryFromPool(pool, nil)
	ctx := t.Context()
	_, err := pool.Exec(ctx, `CREATE FUNCTION reject_receipt_delivery() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected rollback'; END $$;
 CREATE TRIGGER reject_receipt_delivery BEFORE INSERT ON alarm_dispatch_deliveries FOR EACH ROW EXECUTE FUNCTION reject_receipt_delivery()`)
	require.NoError(t, err)

	result, err := repository.InsertBatch(ctx, PublishBatchInput{Envelopes: []domain.AlarmQueueEnvelope{receiptEnvelope("room-rollback", "canonical")}})
	require.ErrorContains(t, err, "injected rollback")
	require.Empty(t, result.Receipts)
	require.Zero(t, result.InsertedDeliveries)
	require.Zero(t, result.InsertedEvents)

	var count int

	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM alarm_dispatch_events`).Scan(&count))
	require.Zero(t, count)
}
