package dispatchoutbox_test

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/park285/shared-go/v2/pkg/db/pgxdb"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/dispatchoutbox"
	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestExpiredLeaseRecoveryKeepsSendUnitAtomic(t *testing.T) {
	for _, test := range []struct {
		name          string
		activeSibling bool
		lockedUnit    bool
	}{
		{name: "first unit exceeds row limit"},
		{name: "one member lease remains valid", activeSibling: true},
		{name: "send request owns unit lock", lockedUnit: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := t.Context()
			pool := dbtest.NewPool(t)
			repo := newOutboxRepository(t, pool)
			_, err := repo.InsertBatch(ctx, dispatchoutbox.PublishBatchInput{Envelopes: []domain.AlarmQueueEnvelope{
				sendUnitTestEnvelope("recover-pair", "recover-a"), sendUnitTestEnvelope("recover-pair", "recover-b"),
			}})
			require.NoError(t, err)

			rows, err := repo.ClaimDue(ctx, "old-owner", 2, time.Minute)
			require.NoError(t, err)
			require.Len(t, rows, 2)
			require.Equal(t, rows[0].SendUnitID, rows[1].SendUnitID)

			_, err = pool.Exec(ctx, `UPDATE alarm_dispatch_deliveries SET locked_at=NOW()-INTERVAL '2 minutes',lock_expires_at=NOW()-INTERVAL '1 minute'`)
			require.NoError(t, err)

			if test.activeSibling {
				_, err = pool.Exec(ctx, `UPDATE alarm_dispatch_deliveries SET lock_expires_at=NOW()+INTERVAL '1 minute' WHERE id=$1`, rows[1].ID)
				require.NoError(t, err)
			}

			if test.lockedUnit {
				assertRecoverySkipsLockedSendUnit(t, pool, repo, rows[0].SendUnitID)
			} else if test.activeSibling {
				recovered, recoveryErr := repo.RecoverExpiredLeased(ctx, 1)
				require.NoError(t, recoveryErr)
				require.Zero(t, recovered)

				_, err = pool.Exec(ctx, `UPDATE alarm_dispatch_deliveries SET lock_expires_at=NOW()-INTERVAL '1 minute'`)
				require.NoError(t, err)
			}

			recovered, err := repo.RecoverExpiredLeased(ctx, 1)
			require.NoError(t, err)
			require.Equal(t, 2, recovered)

			claimed, err := repo.ClaimDue(ctx, "new-owner", 1, time.Minute)
			require.NoError(t, err)
			require.Len(t, claimed, 2)

			claims := []dispatchoutbox.DeliveryClaim{
				{ID: claimed[0].ID, AttemptCount: claimed[0].AttemptCount},
				{ID: claimed[1].ID, AttemptCount: claimed[1].AttemptCount},
			}

			_, err = repo.PinSendRequest(ctx, claimed[0].SendUnitID, claims, "new-owner", claimed[0].ClientRequestID, dispatchoutbox.SendRequest{Body: "both members", Route: dispatchoutbox.SendRouteText})
			require.NoError(t, err)
		})
	}
}

func assertRecoverySkipsLockedSendUnit(t *testing.T, pool *pgxpool.Pool, repo *dispatchoutbox.PgxRepository, unitID int64) {
	t.Helper()

	ctx := t.Context()
	tx, beginErr := pool.Begin(ctx)
	require.NoError(t, beginErr)

	defer func() {
		rollbackErr := pgxdb.Rollback(ctx, tx)
		if rollbackErr != nil {
			require.NoError(t, rollbackErr)
		}
	}()

	_, err := tx.Exec(ctx, `SELECT id FROM alarm_dispatch_send_units WHERE id=$1 FOR UPDATE`, unitID)
	require.NoError(t, err)

	recovered, recoveryErr := repo.RecoverExpiredLeased(ctx, 1)
	require.NoError(t, recoveryErr)
	require.Zero(t, recovered)
	require.NoError(t, pgxdb.Rollback(ctx, tx))
}
