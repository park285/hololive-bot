package collectorruntime

import (
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	dbtest "github.com/kapu/hololive-dbtest"
)

func TestPendingObservationCountBoundsBothActiveStates(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)

	var seeded int

	if err := pool.QueryRow(ctx, mustTestSQL("seed_observation_queue.sql"), 5, 4, 40, "pending-count").Scan(&seeded); err != nil {
		t.Fatal(err)
	}

	if seeded != 49 {
		t.Fatalf("seeded rows = %d, want 49", seeded)
	}

	store := &postgresQueueStore{pool: pool}

	exact, err := store.CountPending(ctx, 20)
	if err != nil {
		t.Fatal(err)
	}

	if exact.Value != 9 || exact.Capped {
		t.Fatalf("exact pending count = %+v, want value 9 uncapped", exact)
	}

	bounded, err := store.CountPending(ctx, 6)
	if err != nil {
		t.Fatal(err)
	}

	if bounded.Value != 6 || !bounded.Capped {
		t.Fatalf("bounded pending count = %+v, want value 6 capped", bounded)
	}
}

func TestPendingObservationCountPlanUsesBothPartialIndexes(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}

	defer func() {
		if rollbackErr := tx.Rollback(ctx); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			t.Errorf("rollback plan transaction: %v", rollbackErr)
		}
	}()

	if _, execErr := tx.Exec(ctx, "SET LOCAL enable_seqscan = off"); execErr != nil {
		t.Fatal(execErr)
	}

	rows, err := tx.Query(ctx, "EXPLAIN (COSTS OFF) "+mustSQL("pending_observation_count.sql"), pendingQueueCap)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	var plan strings.Builder

	for rows.Next() {
		var line string

		if err := rows.Scan(&line); err != nil {
			t.Fatal(err)
		}

		plan.WriteString(line)
		plan.WriteByte('\n')
	}

	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}

	t.Logf("pending observation count plan:\n%s", plan.String())

	for _, index := range []string{"idx_source_observation_queue_claim", "idx_source_observation_queue_lease_recovery"} {
		if !strings.Contains(plan.String(), index) {
			t.Fatalf("pending count plan does not use %s:\n%s", index, plan.String())
		}
	}
}
