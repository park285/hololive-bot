package store

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/lifecycle"
	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestTransitionClaimPendingSQLPreparedModesPreservePendingGuard(t *testing.T) {
	for _, mode := range preparedPlanModes {
		t.Run(mode, func(t *testing.T) {
			pool := dbtest.NewPool(t)
			pendingID, failedID := seedTransitionLogicalGroup(t, pool, "video-claim-plan", "room-claim-plan")
			sentID, sendingID := seedTransitionLogicalGroup(t, pool, "video-claim-plan-other", "room-claim-plan")
			at := time.Now().UTC().Truncate(time.Microsecond)
			staleLock := at.Add(-2 * time.Hour)

			setTransitionDeliveryState(t, pool, failedID, lifecycle.StatusFailed, nil)
			setTransitionDeliveryState(t, pool, sentID, lifecycle.StatusSent, nil)
			setTransitionDeliveryState(t, pool, sendingID, lifecycle.StatusSending, &staleLock)

			tx := preparePlanModeSQLTest(t, pool, mode, "claim_pending_sql_v1", "transition_claim_pending.sql")
			run := func(status any) []domain.YouTubeNotificationDelivery {
				t.Helper()

				rows, err := tx.Query(t.Context(), "claim_pending_sql_v1", status, at.Add(-time.Minute),
					at, at.Add(-time.Hour), 10, at)
				require.NoError(t, err)

				claimed, err := pgx.CollectRows(rows, scanTransitionDelivery)
				require.NoError(t, err)

				return claimed
			}

			for _, rejected := range []any{
				lifecycle.StatusFailed, lifecycle.StatusSent, lifecycle.StatusSending, nil, "PENDING' OR 1=1 --",
			} {
				require.Empty(t, run(rejected), "status %v must not be claimed", rejected)
			}

			claimed := run(lifecycle.StatusPending)
			require.Len(t, claimed, 1)
			require.Equal(t, pendingID, claimed[0].ID)
			require.NotNil(t, claimed[0].LockedAt)
			require.Empty(t, run(lifecycle.StatusPending))

			requirePreparedPlanMode(t, tx, "claim_pending_sql_v1", mode)
		})
	}
}

func TestTransitionStaleSendingSQLPreparedModesPreserveSendingGuard(t *testing.T) {
	for _, mode := range preparedPlanModes {
		t.Run(mode, func(t *testing.T) {
			pool := dbtest.NewPool(t)
			sendingID, failedID := seedTransitionLogicalGroup(t, pool, "video-stale-plan", "room-stale-plan")
			sentID, pendingID := seedTransitionLogicalGroup(t, pool, "video-stale-plan-other", "room-stale-plan")
			at := time.Now().UTC().Truncate(time.Microsecond)
			staleLock := at.Add(-2 * time.Hour)

			setTransitionDeliveryState(t, pool, sendingID, lifecycle.StatusSending, &staleLock)
			setTransitionDeliveryState(t, pool, failedID, lifecycle.StatusFailed, &staleLock)
			setTransitionDeliveryState(t, pool, sentID, lifecycle.StatusSent, &staleLock)
			setTransitionDeliveryState(t, pool, pendingID, lifecycle.StatusPending, &staleLock)

			tx := preparePlanModeSQLTest(t, pool, mode, "stale_sending_sql_v1", "transition_stale_sending.sql")
			run := func(status any) []int64 {
				t.Helper()

				rows, err := tx.Query(t.Context(), "stale_sending_sql_v1", status, at.Add(-time.Hour), 10)
				require.NoError(t, err)

				ids, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (int64, error) {
					values, valuesErr := row.Values()
					if valuesErr != nil {
						return 0, valuesErr
					}

					id, ok := values[0].(int64)
					require.True(t, ok, "stale sending SQL must project delivery.id first")

					return id, nil
				})
				require.NoError(t, err)

				return ids
			}

			for _, rejected := range []any{
				lifecycle.StatusFailed, lifecycle.StatusSent, lifecycle.StatusPending, nil, "SENDING' OR 1=1 --",
			} {
				require.Empty(t, run(rejected), "status %v must not be loaded as stale sending", rejected)
			}

			require.Equal(t, []int64{sendingID}, run(lifecycle.StatusSending))

			requirePreparedPlanMode(t, tx, "stale_sending_sql_v1", mode)
		})
	}
}

func setTransitionDeliveryState(
	t *testing.T,
	pool *pgxpool.Pool,
	deliveryID int64,
	status lifecycle.DeliveryStatus,
	lockedAt *time.Time,
) {
	t.Helper()

	_, err := pool.Exec(t.Context(), `
		UPDATE youtube_notification_delivery
		SET status = $1, locked_at = $2
		WHERE id = $3
	`, status, lockedAt, deliveryID)
	require.NoError(t, err)
}
