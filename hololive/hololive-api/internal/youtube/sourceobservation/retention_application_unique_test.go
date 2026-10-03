package sourceobservation

import (
	"testing"

	dbtest "github.com/kapu/hololive-dbtest"
)

func TestApplicationPartialUniqueKeepsActiveIdempotenceAndOrphanHistory(t *testing.T) {
	pool := dbtest.NewPool(t)
	ids := publishProcessedObservations(t.Context(), t, pool, NewRepository(pool), 2)
	insert := `
		INSERT INTO source_observation_applications (
			observation_id, provider, observation_kind, subject_key, evidence_sha256,
			entity_kind, entity_key, decision, effective_at
		) VALUES ($1, 'youtubejs', 'community_page', 'UC_RETENTION', repeat('a', 64),
			'retention_test', 'same-entity', 'APPLIED', NOW())
		ON CONFLICT (observation_id, entity_kind, entity_key)
			WHERE observation_id IS NOT NULL DO NOTHING`

	first, err := pool.Exec(t.Context(), insert, ids[0])
	if err != nil || first.RowsAffected() != 1 {
		t.Fatalf("first active application: %v rows=%d", err, first.RowsAffected())
	}

	duplicate, err := pool.Exec(t.Context(), insert, ids[0])
	if err != nil || duplicate.RowsAffected() != 0 {
		t.Fatalf("duplicate active application: %v rows=%d", err, duplicate.RowsAffected())
	}

	second, err := pool.Exec(t.Context(), insert, ids[1])
	if err != nil || second.RowsAffected() != 1 {
		t.Fatalf("second observation application: %v rows=%d", err, second.RowsAffected())
	}

	for range 2 {
		orphan, err := pool.Exec(t.Context(), insert, nil)
		if err != nil || orphan.RowsAffected() != 1 {
			t.Fatalf("orphan history insert: %v rows=%d", err, orphan.RowsAffected())
		}
	}

	var active, orphan int

	if err := pool.QueryRow(t.Context(), `
		SELECT count(*) FILTER (WHERE observation_id IS NOT NULL),
		       count(*) FILTER (WHERE observation_id IS NULL)
		FROM source_observation_applications
		WHERE entity_kind='retention_test' AND entity_key='same-entity'
	`).Scan(&active, &orphan); err != nil {
		t.Fatal(err)
	}

	if active != 2 || orphan != 2 {
		t.Fatalf("applications: active=%d orphan=%d, want 2 each", active, orphan)
	}
}
