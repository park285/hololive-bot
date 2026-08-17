package joblease

import (
	"context"
	"testing"
	"time"

	dbtest "github.com/kapu/hololive-dbtest"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

func TestProjectionTargetSnapshotSQLBoundsOnlyNonNullRows(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.NewPool(t)
	generation := seedProjection(t, pool, []leaseTarget{
		{"UC_A", contract.KindLiveSnapshot, time.Minute, true},
		{"UC_B", contract.KindLiveSnapshot, time.Minute, true},
		{"UC_C", contract.KindLiveSnapshot, time.Minute, true},
	})
	requested := []string{string(contract.KindLiveSnapshot), string(contract.KindSchedule)}
	rows, err := pool.Query(ctx, mustSQL("repository_target_snapshot_projection_0144_16.sql"), generation, requested, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	sentinels := map[contract.ObservationKind]int{}
	nonNull := 0
	for rows.Next() {
		var kind contract.ObservationKind
		var current bool
		var subject *string
		if err := rows.Scan(&kind, &current, &subject); err != nil {
			t.Fatal(err)
		}
		if !current {
			t.Fatal("current projection row was marked stale")
		}
		if subject == nil {
			sentinels[kind]++
		} else {
			nonNull++
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if nonNull != 2 {
		t.Fatalf("non-null roster rows = %d, want cap+1 = 2", nonNull)
	}
	if sentinels[contract.KindLiveSnapshot] != 1 || sentinels[contract.KindSchedule] != 1 || len(sentinels) != 2 {
		t.Fatalf("empty-kind sentinels = %#v, want one per requested kind", sentinels)
	}
}
