package dispatchoutbox

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	dbtest "github.com/kapu/hololive-dbtest"
)

const staleClaimPauseKey = 20261123

func TestClaimDueStaleSnapshotDoesNotReleaseSettledDelivery(t *testing.T) {
	status := claimAfterConcurrentSettle(t, `UPDATE alarm_dispatch_deliveries
		SET status='sent', sent_at=clock_timestamp(), locked_by=NULL, locked_at=NULL, lock_expires_at=NULL WHERE id=$1`)
	require.Equal(t, string(StatusSent), status)
}

func TestClaimDueStaleSnapshotDoesNotSkipFutureRetry(t *testing.T) {
	status := claimAfterConcurrentSettle(t, `UPDATE alarm_dispatch_deliveries
		SET status='retry', next_attempt_at=clock_timestamp() + interval '1 hour', locked_by=NULL, locked_at=NULL, lock_expires_at=NULL WHERE id=$1`)
	require.Equal(t, string(StatusRetry), status)
}

func claimAfterConcurrentSettle(t *testing.T, settle string) string {
	t.Helper()

	pool := dbtest.NewPool(t)
	repo := newTestRepository(t, pool)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)

	defer cancel()

	_, err := repo.InsertBatch(ctx, PublishBatchInput{Envelopes: []AlarmQueueEnvelope{receiptEnvelope("stale-claim-room", "stale")}})
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `CREATE SCHEMA stale_claim_pause;
		CREATE FUNCTION stale_claim_pause.clock_timestamp() RETURNS timestamptz LANGUAGE plpgsql VOLATILE AS $f$
		BEGIN
			PERFORM pg_catalog.pg_advisory_lock_shared(`+"20261123"+`);
			PERFORM pg_catalog.pg_advisory_unlock_shared(`+"20261123"+`);
			RETURN pg_catalog.clock_timestamp();
		END $f$`)
	require.NoError(t, err)

	pausedConfig := pool.Config()

	pausedConfig.MaxConns = 1
	pausedConfig.ConnConfig.RuntimeParams["search_path"] = "stale_claim_pause, pg_catalog, public"

	pausedPool, err := pgxpool.NewWithConfig(ctx, pausedConfig)
	require.NoError(t, err)
	t.Cleanup(pausedPool.Close)

	stale := newTestRepository(t, pausedPool)

	holder, err := pool.Acquire(ctx)
	require.NoError(t, err)

	defer holder.Release()

	_, err = holder.Exec(ctx, "SELECT pg_advisory_lock($1)", staleClaimPauseKey)
	require.NoError(t, err)

	done := make(chan error, 1)

	var (
		staleClaimed []*Record
		workers      sync.WaitGroup
	)

	workers.Go(func() {
		var claimErr error

		staleClaimed, claimErr = stale.ClaimDue(ctx, "stale-worker", 10, time.Minute)
		done <- claimErr
	})

	defer func() {
		cancel()
		workers.Wait()
	}()

	waitFailureClockLock(ctx, t, pool, holder.Conn().PgConn().PID(), done)

	claimed, err := repo.ClaimDue(ctx, "fresh-worker", 10, time.Minute)
	require.NoError(t, err)
	require.Len(t, claimed, 1)

	_, err = pool.Exec(ctx, settle, claimed[0].ID)
	require.NoError(t, err)

	_, err = holder.Exec(ctx, "SELECT pg_advisory_unlock($1)", staleClaimPauseKey)
	require.NoError(t, err)
	require.NoError(t, <-done)
	require.Empty(t, staleClaimed)

	var status string

	require.NoError(t, pool.QueryRow(ctx, "SELECT status FROM alarm_dispatch_deliveries WHERE id=$1", claimed[0].ID).Scan(&status))

	return status
}
