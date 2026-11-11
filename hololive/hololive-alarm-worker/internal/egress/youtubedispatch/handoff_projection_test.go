package youtubedispatch

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/store"
	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestYouTubeHandoffProjectionFailureRollsBackSettlement(t *testing.T) {
	fixture := sendYouTubeHandoffFixture(t)
	fixture.makeDue(t, 0)

	_, err := fixture.pool.Exec(t.Context(), `CREATE FUNCTION reject_handoff_projection() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN RAISE EXCEPTION 'injected aggregate projection failure'; END $$;
		CREATE TRIGGER reject_handoff_projection BEFORE UPDATE ON youtube_notification_outbox
		FOR EACH ROW WHEN (NEW.status='FAILED') EXECUTE FUNCTION reject_handoff_projection()`)
	require.NoError(t, err)

	reconciler := fixture.reconciler(t, &youtubeStatusClient{snapshot: youtubeStatus("outcome_unknown")}, 3)

	_, err = reconciler.RunOnce(t.Context())
	require.NoError(t, err)

	failed := fixture.load(t)
	require.Equal(t, domain.OutboxStatusSent, failed.deliveryStatus)
	require.Equal(t, domain.OutboxStatusSent, failed.outboxStatus)
	require.Nil(t, failed.checkedAt)
	require.NotNil(t, failed.nextCheck)

	var ledgerStatus string

	require.NoError(t, fixture.pool.QueryRow(t.Context(), `SELECT status FROM youtube_notification_delivery_ledger WHERE source_delivery_id=$1`, fixture.deliveryID).Scan(&ledgerStatus))
	require.Equal(t, "SENT", ledgerStatus)

	_, err = fixture.pool.Exec(t.Context(), `DROP TRIGGER reject_handoff_projection ON youtube_notification_outbox`)
	require.NoError(t, err)

	fixture.makeDue(t, 0)

	_, err = reconciler.RunOnce(t.Context())
	require.NoError(t, err)

	settled := fixture.load(t)
	require.Equal(t, store.DeliveryStatusQuarantined, settled.deliveryStatus)
	require.Equal(t, domain.OutboxStatusFailed, settled.outboxStatus)
	require.NotNil(t, settled.checkedAt)
	require.Nil(t, settled.nextCheck)
}
