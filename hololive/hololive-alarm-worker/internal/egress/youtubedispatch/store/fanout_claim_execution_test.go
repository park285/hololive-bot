package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/deliverysql"
)

func TestFanoutSQLPreparedModesPreservePendingGuard(t *testing.T) {
	for _, mode := range []string{"force_generic_plan", "force_custom_plan"} {
		t.Run(mode, func(t *testing.T) {
			pool := dbtest.NewPool(t)
			failedID := seedTransitionFanoutOutbox(t, pool, "video-sql-failed")
			sentID := seedTransitionFanoutOutbox(t, pool, "video-sql-sent")
			_, err := pool.Exec(t.Context(), `UPDATE youtube_notification_outbox
                SET status = 'FAILED', attempt_count = 3, terminal_at = NOW(), error = 'synthetic failure'
                WHERE id = $1`, failedID)
			require.NoError(t, err)
			_, err = pool.Exec(t.Context(), `UPDATE youtube_notification_outbox
                SET status = 'SENT', sent_at = NOW(), terminal_at = NOW()
                WHERE id = $1`, sentID)
			require.NoError(t, err)
			id := seedTransitionFanoutOutbox(t, pool, "video-sql-plan")
			tx := prepareFanoutSQLTest(t, pool, mode)
			at := time.Now().UTC().Truncate(time.Microsecond)

			for _, rejected := range []any{domain.OutboxStatusFailed, domain.OutboxStatusSent, nil, "PENDING' OR 1=1 --"} {
				rows := runFanoutSQLTest(t, tx, rejected, at)
				require.Empty(t, rows)
			}

			var plan []byte
			require.NoError(t, tx.QueryRow(t.Context(), `EXPLAIN (FORMAT JSON, COSTS ON)
                EXECUTE fanout_sql_v2('PENDING', NOW() - INTERVAL '1 minute', NOW(),
                NOW() - INTERVAL '1 hour', 1, NOW())`).Scan(&plan))
			t.Logf("%s fanout plan: %s", mode, plan)

			claimed := runFanoutSQLTest(t, tx, domain.OutboxStatusPending, at)
			require.Len(t, claimed, 1)
			require.Equal(t, id, claimed[0].ID)
			require.NotNil(t, claimed[0].LockedAt)
			require.Empty(t, runFanoutSQLTest(t, tx, domain.OutboxStatusPending, at))

			var genericPlans, customPlans int64
			require.NoError(t, tx.QueryRow(t.Context(), `SELECT generic_plans, custom_plans
                FROM pg_prepared_statements WHERE name = 'fanout_sql_v2'`).Scan(&genericPlans, &customPlans))
			if mode == "force_generic_plan" {
				require.Positive(t, genericPlans)
				require.Zero(t, customPlans)
			} else {
				require.Positive(t, customPlans)
				require.Zero(t, genericPlans)
			}
		})
	}
}

func TestFanoutSQLSkipsLockedRowAndReclaimsAfterRollback(t *testing.T) {
	pool := dbtest.NewPool(t)
	seedCompletedLedgerState(t, pool)
	firstID := seedTransitionFanoutOutbox(t, pool, "video-sql-locked")
	secondID := seedTransitionFanoutOutbox(t, pool, "video-sql-free")
	transition := newTestTransitionStore(t, pool)
	tx, err := pool.Begin(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() { rollbackFanoutSQLTest(t, tx) })

	var lockedID int64
	require.NoError(t, tx.QueryRow(t.Context(), `SELECT id FROM youtube_notification_outbox
        WHERE id = $1 FOR UPDATE`, firstID).Scan(&lockedID))
	require.Equal(t, firstID, lockedID)

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	claimed, err := transition.ClaimOutboxesForFanout(ctx, 2)
	require.NoError(t, err)
	require.Len(t, claimed, 1)
	require.Equal(t, secondID, claimed[0].ID)

	require.NoError(t, tx.Rollback(t.Context()))
	claimed, err = transition.ClaimOutboxesForFanout(t.Context(), 2)
	require.NoError(t, err)
	require.Len(t, claimed, 1)
	require.Equal(t, firstID, claimed[0].ID)
}

func prepareFanoutSQLTest(t *testing.T, pool *pgxpool.Pool, mode string) pgx.Tx {
	t.Helper()

	tx, err := pool.Begin(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() { rollbackFanoutSQLTest(t, tx) })
	_, err = tx.Exec(t.Context(), `SELECT set_config('plan_cache_mode', $1, true)`, mode)
	require.NoError(t, err)
	_, err = tx.Exec(t.Context(), `SET LOCAL statement_timeout = '5s'`)
	require.NoError(t, err)
	_, err = tx.Conn().Prepare(t.Context(), "fanout_sql_v2", mustSQL("fanout_claim.sql"))
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), 5*time.Second)
		defer cancel()
		if err := tx.Conn().Deallocate(ctx, "fanout_sql_v2"); err != nil {
			t.Errorf("deallocate fanout SQL test: %v", err)
		}
	})
	return tx
}

func runFanoutSQLTest(t *testing.T, tx pgx.Tx, status any, at time.Time) []domain.YouTubeNotificationOutbox {
	t.Helper()

	rows, err := tx.Query(t.Context(), "fanout_sql_v2", status, at.Add(-time.Minute),
		at, at.Add(-time.Hour), 1, at)
	require.NoError(t, err)
	claimed, err := pgx.CollectRows(rows, deliverysql.ScanOutboxRow)
	require.NoError(t, err)
	return claimed
}

func rollbackFanoutSQLTest(t *testing.T, tx pgx.Tx) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), 5*time.Second)
	defer cancel()
	if err := tx.Rollback(ctx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
		t.Errorf("rollback fanout SQL test: %v", err)
	}
}
