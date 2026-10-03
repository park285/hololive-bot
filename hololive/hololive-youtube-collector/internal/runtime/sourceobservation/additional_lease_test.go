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

	if _, err := pool.Exec(t.Context(), `
		INSERT INTO youtube_collection_targets (
			projection_generation, subject_key, observation_kind,
			priority, poll_interval_ms, enabled, valid_until
		) VALUES ($1, $2, $3, 50, 60000, TRUE, NOW() + INTERVAL '1 day')
		ON CONFLICT (projection_generation, subject_key, observation_kind) DO NOTHING
	`, proof.ProjectionGeneration, subjectKey, kind); err != nil {
		t.Fatalf("seed additional target: %v", err)
	}

	jobClass := "SUBJECT"

	if jobKind == "holodex_metadata" || jobKind == "official_schedule" {
		jobClass = "GLOBAL"
	}

	if _, err := pool.Exec(t.Context(), `
		INSERT INTO youtube_collection_job_leases (
			job_key, provider, job_class, collection_job_kind, subject_key,
			projection_generation, poll_interval_ms, slot_state, scheduled_for,
			next_due_at, fence_epoch, owner_instance, lease_expires_at
		) VALUES ($1, $2, $3, $4, $5, $6, 60000, 'ACTIVE', $7, $7, $8, $9, NOW() + INTERVAL '1 hour')
	`, proof.JobKey, provider, jobClass, jobKind, subjectKey, proof.ProjectionGeneration,
		proof.ScheduledFor, proof.FenceEpoch, proof.OwnerInstance); err != nil {
		t.Fatalf("seed additional lease: %v", err)
	}

	return proof
}
