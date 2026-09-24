package runtime

import (
	_ "embed"
	"errors"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	dbtest "github.com/kapu/hololive-dbtest"
)

//go:embed testqueries/collection_target_observability.sql
var collectionTargetFixture string

func TestCollectionTargetSnapshotIncludesUnqueuedAndExcludesInactive(t *testing.T) {
	pool := dbtest.NewPool(t)
	if _, err := pool.Exec(t.Context(), collectionTargetFixture); err != nil {
		t.Fatal(err)
	}

	rows, err := pool.Query(t.Context(), mustSQL("collection_target_observability.sql"))
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	samples, err := scanCollectionTargets(rows)
	if err != nil {
		t.Fatal(err)
	}

	if len(samples) != 5 {
		t.Fatalf("sample count = %d", len(samples))
	}

	for i := range samples {
		s := &samples[i]
		switch s.kind {
		case "youtubejs_viewer":
			got := [...]int64{s.targets, s.stale, s.neverCompleted, s.due, s.viewerLive, s.viewerUpcoming, s.viewerStateMismatch, s.viewerPastDue, s.viewerPastDue7d}
			if got != [...]int64{4, 2, 1, 2, 2, 2, 1, 1, 1} {
				t.Fatalf("viewer snapshot = %+v", s)
			}

			if s.oldestCompletionAge < 600 || s.oldestDueAge < 600 || s.requiredRate != 4.0/120 {
				t.Fatalf("viewer age/rate = %+v", s)
			}
		case "youtubejs_content":
			if s.targets != 1 || s.requiredRate != 2.0/120 {
				t.Fatalf("content bundle was counted twice: %+v", s)
			}
		}
	}

	m := newCollectionTargetMetrics(prometheus.NewPedanticRegistry())
	now := time.Now()
	m.observe(samples, now, nil)
	m.observe(nil, now.Add(time.Minute), errors.New("database unavailable"))

	if testutil.ToFloat64(m.success) != 0 || testutil.ToFloat64(m.targets.WithLabelValues("youtubejs_viewer")) != 4 || testutil.ToFloat64(m.lastSuccess) != float64(now.Unix()) {
		t.Fatal("failed observation replaced valid snapshot or timestamp")
	}
}

func TestCollectionTargetSnapshotRejectsMissingProjection(t *testing.T) {
	pool := dbtest.NewPool(t)

	rows, err := pool.Query(t.Context(), mustSQL("collection_target_observability.sql"))
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	if _, err := scanCollectionTargets(rows); err == nil {
		t.Fatal("missing current projection was reported as a valid empty fleet")
	}
}
