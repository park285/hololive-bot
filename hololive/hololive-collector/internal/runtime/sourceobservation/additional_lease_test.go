package sourceobservation

import (
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

func seedAdditionalLease(
	t *testing.T,
	pool *pgxpool.Pool,
	existing *contract.LeaseProof,
	provider contract.Provider,
	kind contract.ObservationKind,
	subjectKey string,
	jobKind string,
) contract.LeaseProof {
	t.Helper()

	proof := *existing

	proof.JobKey = "job:" + jobKind + ":" + subjectKey
	proof.CollectionJobKind = jobKind

	insertPublishTarget(t.Context(), t, pool, proof.ProjectionGeneration, subjectKey, kind, true)

	jobClass := "SUBJECT"

	if jobKind == "holodex_metadata" || jobKind == "official_schedule" {
		jobClass = "GLOBAL"
	}

	scope := publishFixtureScope(t, provider, jobKind)

	if _, err := pool.Exec(t.Context(), `
		INSERT INTO youtube_collection_job_leases (
			job_key, provider, job_class, collection_job_kind, subject_key,
			projection_generation, poll_interval_ms, slot_state, scheduled_for,
			next_due_at, fence_epoch, owner_instance, lease_expires_at,
			membership_kinds, membership_exact_subject
		) VALUES ($1, $2, $3, $4, $5, $6, 60000, 'ACTIVE', $7, $7, $8, $9, NOW() + INTERVAL '1 hour', $10::text[], $11)
	`, proof.JobKey, provider, jobClass, jobKind, subjectKey, proof.ProjectionGeneration,
		proof.ScheduledFor, proof.FenceEpoch, proof.OwnerInstance, scope.Kinds, scope.ExactSubject); err != nil {
		t.Fatalf("seed additional lease: %v", err)
	}

	resyncPublishMembership(t.Context(), t, pool)

	return proof
}
