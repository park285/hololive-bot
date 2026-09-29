package targetprojection

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	dbtest "github.com/kapu/hololive-dbtest"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

func TestRetainBoundsRowsWithinOneLargeRetiredGeneration(t *testing.T) {
	pool := dbtest.NewPool(t)

	refresher, err := NewRefresher(pool, time.Hour)
	require.NoError(t, err)

	targets := []TargetSpec{
		{SubjectKey: "retention:old", ObservationKind: contract.KindCommunityPage, Priority: 50, PollInterval: time.Minute, Enabled: true},
		{SubjectKey: "retention:second", ObservationKind: contract.KindCommunityPage, Priority: 50, PollInterval: time.Minute, Enabled: true},
	}
	reasons := make([]TargetReason, 27)

	for i := range reasons {
		reasons[i] = TargetReason{
			SubjectKey: targets[0].SubjectKey, ObservationKind: targets[0].ObservationKind,
			ReasonKind: "retention_test", ReasonKey: fmt.Sprintf("room-%03d", i),
		}
	}

	retired := mustRefresh(t, refresher, staticBuilder{targets: targets, reasons: reasons}, projectionNow, "large retired generation")
	currentTarget := targets[0]
	currentTarget.Priority++

	current := mustRefresh(t, refresher, staticBuilder{targets: []TargetSpec{currentTarget}}, projectionNow.Add(time.Minute), "current generation")

	young, err := refresher.Retain(t.Context(), projectionNow.Add(24*time.Hour), 24*time.Hour, 4)
	require.NoError(t, err)

	if young.ReasonsDeleted != 0 || young.TargetsDeleted != 0 || young.GenerationsDeleted != 0 {
		t.Fatalf("deleted generation before cutoff: %#v", young)
	}

	for tick := range 12 {
		result, err := refresher.Retain(t.Context(), projectionNow.Add(4*24*time.Hour), 24*time.Hour, 4)
		if err != nil {
			t.Fatalf("retention tick %d: %v", tick, err)
		}

		if result.ReasonsDeleted+result.TargetsDeleted > 4 || result.GenerationsDeleted > 1 {
			t.Fatalf("unbounded retention tick %d: %#v", tick, result)
		}

		if tick == 0 && (result.ReasonsDeleted != 4 || result.TargetsDeleted != 0 || result.GenerationsDeleted != 0) {
			t.Fatalf("first tick deleted a whole generation: %#v", result)
		}

		if result.GenerationsDeleted == 1 {
			if tick < 6 {
				t.Fatalf("large generation deleted before all rows were budgeted: tick %d", tick)
			}

			break
		}

		if tick == 11 {
			t.Fatal("bounded cleanup never removed the empty retired generation")
		}
	}

	assertGenerationMissing(t, pool, retired.Generation)
	assertGenerationStatus(t, pool, current.Generation, "CURRENT")
	assertTargetCount(t, pool, current.Generation, 1)
}

func TestRetainDoesNotDeleteGenerationAcrossConcurrentLeaseInsertion(t *testing.T) {
	pool := dbtest.NewPool(t)

	refresher, err := NewRefresher(pool, time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	target := TargetSpec{SubjectKey: "retention:lease", ObservationKind: contract.KindCommunityPage, Priority: 50, PollInterval: time.Minute, Enabled: true}
	retired := mustRefresh(t, refresher, staticBuilder{targets: []TargetSpec{target}}, projectionNow, "lease candidate")
	target.Priority++
	mustRefresh(t, refresher, staticBuilder{targets: []TargetSpec{target}}, projectionNow.Add(time.Minute), "lease current")

	tx, err := pool.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	defer func() {
		if rollbackErr := tx.Rollback(t.Context()); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			t.Error(rollbackErr)
		}
	}()

	_, err = tx.Exec(t.Context(), `SELECT generation FROM youtube_collection_projection_generations WHERE generation=$1 FOR UPDATE`, retired.Generation)
	require.NoError(t, err)

	result, err := refresher.Retain(t.Context(), projectionNow.Add(4*24*time.Hour), 24*time.Hour, 4)
	if err != nil {
		t.Fatal(err)
	}

	if result.GenerationsDeleted != 0 || result.ReasonsDeleted != 0 || result.TargetsDeleted != 0 {
		t.Fatalf("cleanup touched concurrently locked generation: %#v", result)
	}

	_, err = tx.Exec(t.Context(), `
		INSERT INTO youtube_collection_job_leases (
			job_key, provider, job_class, collection_job_kind, subject_key,
			projection_generation, poll_interval_ms, slot_state, scheduled_for, next_due_at,
			owner_instance, lease_expires_at
		) VALUES ('retention-racing-lease', 'youtubejs', 'SUBJECT', 'youtubejs_community',
			'retention:lease', $1, 60000, 'ACTIVE', $2, $2, 'collector-a', $3)
	`, retired.Generation, projectionNow, time.Now().Add(7*24*time.Hour))
	require.NoError(t, err)

	require.NoError(t, tx.Commit(t.Context()))

	result, err = refresher.Retain(t.Context(), projectionNow.Add(4*24*time.Hour), 24*time.Hour, 4)
	if err != nil {
		t.Fatal(err)
	}

	if result.GenerationsDeleted != 0 || result.TargetsDeleted != 0 {
		t.Fatalf("cleanup deleted lease-held generation: %#v", result)
	}

	assertGenerationStatus(t, pool, retired.Generation, "RETIRED")
}
