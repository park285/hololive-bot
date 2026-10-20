package store

import (
	"testing"

	"github.com/stretchr/testify/require"

	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestPrepareClaimedRejectsIncompleteGroupWindow(t *testing.T) {
	pool := dbtest.NewPool(t)
	transition := newTestTransitionStore(t, pool)
	ownerID, followerID := seedTransitionLogicalGroup(t, pool, "window-prepare", "room-window-prepare")
	seedLogicalGroupWindowNoise(t, pool, "window-prepare", "room-window-prepare", 11)

	claimed, err := transition.ClaimPending(t.Context(), 1)
	require.NoError(t, err)
	require.Len(t, claimed, 1)
	require.Equal(t, ownerID, claimed[0].ID)

	outboxes := loadTransitionTestOutboxes(t, pool, claimed)

	_, err = transition.PrepareClaimed(t.Context(), claimed, outboxes)
	require.Error(t, err)
	require.Equal(t, claimed[0].Status, loadHandoffTestDelivery(t, pool, ownerID).status)
	require.Equal(t, domain.OutboxStatusPending, loadHandoffTestDelivery(t, pool, followerID).status)
}

func TestStaleQuarantineRejectsIncompleteGroupWindow(t *testing.T) {
	pool := dbtest.NewPool(t)
	transition := newTestTransitionStore(t, pool)
	ownerID, followerID := seedTransitionLogicalGroup(t, pool, "window-stale", "room-window-stale")
	seedLogicalGroupWindowNoise(t, pool, "window-stale", "room-window-stale", 11)

	_, err := pool.Exec(t.Context(), `UPDATE youtube_notification_delivery
		SET status='SENDING', created_at=clock_timestamp()-INTERVAL '2 days',
		    locked_at=clock_timestamp()-INTERVAL '1 day', locked_by='youtube-dispatcher:test', row_version=1
		WHERE id=$1`, ownerID)
	require.NoError(t, err)

	_, err = transition.QuarantineStaleLogicalGroups(t.Context(), 1)
	require.Error(t, err)
	require.Equal(t, DeliveryStatusSending, loadHandoffTestDelivery(t, pool, ownerID).status)
	require.Equal(t, domain.OutboxStatusPending, loadHandoffTestDelivery(t, pool, followerID).status)

	var ledgerRows int

	require.NoError(t, pool.QueryRow(t.Context(), `SELECT count(*) FROM youtube_notification_delivery_ledger
		WHERE source_delivery_id=$1`, ownerID).Scan(&ledgerRows))
	require.Zero(t, ledgerRows)
}
