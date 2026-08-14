package joblease

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/dbx"
	"github.com/kapu/hololive-shared/pkg/service/youtube/sourceobservation"
)

type Repository struct {
	pool      *pgxpool.Pool
	config    Config
	contracts sourceobservation.JobContractSet
}

func NewRepository(pool *pgxpool.Pool, config Config) (*Repository, error) {
	if pool == nil {
		return nil, fmt.Errorf("create collection job lease repository: pool is nil")
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}
	return &Repository{pool: pool, config: config, contracts: sourceobservation.InitialJobContracts()}, nil
}

func (r *Repository) Candidates(ctx context.Context, provider contract.Provider, jobKind string, limit int) ([]JobSpec, error) {
	if r == nil || r.pool == nil || r.contracts == nil || !provider.Valid() || limit < 1 || limit > r.config.AcquisitionBatch {
		return nil, fmt.Errorf("list collection job candidates: %w", ErrInvalidJob)
	}
	definition, ok := r.contracts.Definition(jobKind)
	if !ok {
		return nil, fmt.Errorf("list collection job candidates: %w: unknown job kind", ErrInvalidJob)
	}
	kinds := emissionKinds(definition, provider)
	if len(kinds) == 0 {
		return nil, fmt.Errorf("list collection job candidates: %w: provider has no emissions", ErrInvalidJob)
	}
	var generation int64
	if err := r.pool.QueryRow(ctx, `
		SELECT generation
		FROM youtube_collection_projection_generations
		WHERE status = 'CURRENT' AND valid_until > clock_timestamp()
	`).Scan(&generation); errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrProjectionStale
	} else if err != nil {
		return nil, fmt.Errorf("list collection job candidates: load current projection: %w", err)
	}
	kindValues := make([]string, len(kinds))
	for i := range kinds {
		kindValues[i] = string(kinds[i])
	}
	if definition.Class == "GLOBAL" {
		subject := definition.FixedSubject
		if subject == "" {
			subject = "global:" + jobKind
		}
		interval, err := r.loadCandidateInterval(ctx, generation, kindValues, definition.Membership, subject)
		if err != nil {
			return nil, err
		}
		return []JobSpec{{
			JobKey: "collector:" + string(provider) + ":global", Provider: provider,
			Class: definition.Class, CollectionJobKind: jobKind, SubjectKey: subject, PollInterval: interval,
		}}, nil
	}

	rows, err := r.pool.Query(ctx, `
		SELECT subject_key,
		       MIN(poll_interval_ms),
		       MAX(poll_interval_ms)
		FROM youtube_collection_targets
		WHERE projection_generation = $1
		  AND observation_kind = ANY($2::text[])
		  AND enabled = TRUE
		  AND valid_until > clock_timestamp()
		GROUP BY subject_key
		ORDER BY MAX(priority) DESC, subject_key
		LIMIT $3
	`, generation, kindValues, limit)
	if err != nil {
		return nil, fmt.Errorf("list collection job candidates: query targets: %w", err)
	}
	defer rows.Close()
	result := make([]JobSpec, 0, limit)
	for rows.Next() {
		var subject string
		var minIntervalMS int64
		var maxIntervalMS int64
		if err := rows.Scan(&subject, &minIntervalMS, &maxIntervalMS); err != nil {
			return nil, fmt.Errorf("list collection job candidates: scan target: %w", err)
		}
		if minIntervalMS != maxIntervalMS {
			return nil, fmt.Errorf("list collection job candidates: %w: subject bundle has conflicting poll intervals", ErrInvalidJob)
		}
		jobKey := "collector:" + string(provider) + ":" + jobKind + ":" + subject
		result = append(result, JobSpec{
			JobKey: jobKey, Provider: provider, Class: definition.Class,
			CollectionJobKind: jobKind, SubjectKey: subject,
			PollInterval: time.Duration(minIntervalMS) * time.Millisecond,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list collection job candidates: read targets: %w", err)
	}
	return result, nil
}

func (r *Repository) EnabledSubjects(ctx context.Context, generation int64, kind contract.ObservationKind) ([]string, error) {
	if r == nil || r.pool == nil || generation <= 0 || !kind.Valid() {
		return nil, fmt.Errorf("list enabled collection subjects: %w", ErrInvalidJob)
	}
	rows, err := r.pool.Query(ctx, `
		SELECT subject_key
		FROM youtube_collection_targets
		WHERE projection_generation = $1
		  AND observation_kind = $2
		  AND enabled = TRUE
		  AND valid_until > clock_timestamp()
		ORDER BY subject_key
	`, generation, string(kind))
	if err != nil {
		return nil, fmt.Errorf("list enabled collection subjects: query targets: %w", err)
	}
	defer rows.Close()
	subjects := make([]string, 0)
	for rows.Next() {
		var subject string
		if err := rows.Scan(&subject); err != nil {
			return nil, fmt.Errorf("list enabled collection subjects: scan target: %w", err)
		}
		subjects = append(subjects, subject)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list enabled collection subjects: read targets: %w", err)
	}
	return subjects, nil
}

func (r *Repository) loadCandidateInterval(
	ctx context.Context,
	generation int64,
	kinds []string,
	membership sourceobservation.JobMembership,
	subject string,
) (time.Duration, error) {
	var count int
	var minIntervalMS int64
	var maxIntervalMS int64
	exact := membership == sourceobservation.JobMembershipExactSubject
	err := r.pool.QueryRow(ctx, `
		SELECT COUNT(*),
		       COALESCE(MIN(poll_interval_ms), 0),
		       COALESCE(MAX(poll_interval_ms), 0)
		FROM youtube_collection_targets
		WHERE projection_generation = $1
		  AND observation_kind = ANY($2::text[])
		  AND enabled = TRUE
		  AND valid_until > clock_timestamp()
		  AND (NOT $3 OR subject_key = $4)
	`, generation, kinds, exact, subject).Scan(&count, &minIntervalMS, &maxIntervalMS)
	if err != nil {
		return 0, fmt.Errorf("list collection job candidates: inspect global target set: %w", err)
	}
	if count == 0 {
		return 0, ErrTargetDisabled
	}
	if membership != sourceobservation.JobMembershipCurrentProjection && minIntervalMS != maxIntervalMS {
		return 0, fmt.Errorf("list collection job candidates: %w: global bundle has conflicting poll intervals", ErrInvalidJob)
	}
	return time.Duration(minIntervalMS) * time.Millisecond, nil
}

func (r *Repository) Acquire(ctx context.Context, spec JobSpec, owner string) (*JobLease, error) {
	if r == nil || r.pool == nil || r.contracts == nil {
		return nil, fmt.Errorf("acquire collection job lease: repository is not configured")
	}
	owner = strings.TrimSpace(owner)
	if owner == "" || len(owner) > 128 {
		return nil, fmt.Errorf("acquire collection job lease: %w: owner is outside bounds", ErrInvalidJob)
	}
	definition, kinds, err := spec.validate(r.contracts)
	if err != nil {
		return nil, fmt.Errorf("acquire collection job lease: %w", err)
	}
	proof, err := dbx.InPgxTxWithResult(ctx, r.pool, func(tx dbx.Tx) (contract.LeaseProof, error) {
		return r.acquireTx(ctx, tx, spec, owner, definition, kinds)
	})
	if err != nil {
		return nil, err
	}
	return &JobLease{repository: r, spec: spec, proof: proof}, nil
}

func (r *Repository) acquireTx(
	ctx context.Context,
	tx dbx.Tx,
	spec JobSpec,
	owner string,
	definition sourceobservation.JobContract,
	kinds []contract.ObservationKind,
) (contract.LeaseProof, error) {
	var generation int64
	err := tx.QueryRow(ctx, `
		SELECT generation
		FROM youtube_collection_projection_generations
		WHERE status = 'CURRENT' AND valid_until > clock_timestamp()
		FOR SHARE
	`).Scan(&generation)
	if errors.Is(err, pgx.ErrNoRows) {
		return contract.LeaseProof{}, ErrProjectionStale
	}
	if err != nil {
		return contract.LeaseProof{}, fmt.Errorf("acquire collection job lease: lock current projection: %w", err)
	}
	kindValues := make([]string, len(kinds))
	for i := range kinds {
		kindValues[i] = string(kinds[i])
	}
	var targetCount int
	var minIntervalMS int64
	var maxIntervalMS int64
	exact := definition.Membership == sourceobservation.JobMembershipExactSubject
	err = tx.QueryRow(ctx, `
		SELECT COUNT(*),
		       COALESCE(MIN(poll_interval_ms), 0),
		       COALESCE(MAX(poll_interval_ms), 0)
		FROM youtube_collection_targets
		WHERE projection_generation = $1
		  AND observation_kind = ANY($2::text[])
		  AND enabled = TRUE
		  AND valid_until > clock_timestamp()
		  AND (NOT $3 OR subject_key = $4)
	`, generation, kindValues, exact, spec.SubjectKey).Scan(&targetCount, &minIntervalMS, &maxIntervalMS)
	if err != nil {
		return contract.LeaseProof{}, fmt.Errorf("acquire collection job lease: verify target set: %w", err)
	}
	if targetCount == 0 {
		return contract.LeaseProof{}, ErrTargetDisabled
	}
	if minIntervalMS != spec.PollInterval.Milliseconds() || maxIntervalMS != minIntervalMS {
		return contract.LeaseProof{}, fmt.Errorf("acquire collection job lease: %w: target cadence does not match job", ErrInvalidJob)
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO youtube_collection_job_leases (
			job_key, provider, job_class, collection_job_kind, subject_key,
			projection_generation, poll_interval_ms, scheduled_for, next_due_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, clock_timestamp(), clock_timestamp())
		ON CONFLICT (job_key) DO NOTHING
	`, spec.JobKey, spec.Provider, spec.Class, spec.CollectionJobKind, spec.SubjectKey,
		generation, spec.PollInterval.Milliseconds()); err != nil {
		return contract.LeaseProof{}, fmt.Errorf("acquire collection job lease: create job row: %w", err)
	}
	var storedProvider string
	var storedClass string
	var storedKind string
	var storedSubject string
	err = tx.QueryRow(ctx, `
		SELECT provider, job_class, collection_job_kind, subject_key
		FROM youtube_collection_job_leases
		WHERE job_key = $1
		FOR UPDATE
	`, spec.JobKey).Scan(&storedProvider, &storedClass, &storedKind, &storedSubject)
	if err != nil {
		return contract.LeaseProof{}, fmt.Errorf("acquire collection job lease: lock job row: %w", err)
	}
	if storedProvider != string(spec.Provider) || storedClass != spec.Class || storedKind != spec.CollectionJobKind || storedSubject != spec.SubjectKey {
		return contract.LeaseProof{}, fmt.Errorf("acquire collection job lease: %w: job key is bound to another identity", ErrInvalidJob)
	}

	var proof contract.LeaseProof
	err = tx.QueryRow(ctx, `
		UPDATE youtube_collection_job_leases
		SET owner_instance = $2,
		    fence_epoch = fence_epoch + 1,
		    projection_generation = $3,
		    poll_interval_ms = $4,
		    scheduled_for = CASE
		        WHEN slot_state = 'IDLE' THEN date_bin(
		            $4::bigint * INTERVAL '1 millisecond',
		            clock_timestamp(),
		            next_due_at
		        )
		        ELSE scheduled_for
		    END,
		    slot_state = 'ACTIVE',
		    retry_not_before = NULL,
		    lease_expires_at = clock_timestamp() + ($5::bigint * INTERVAL '1 millisecond'),
		    last_error_code = NULL,
		    updated_at = clock_timestamp()
		WHERE job_key = $1
		  AND (
		      (slot_state = 'IDLE' AND next_due_at <= clock_timestamp())
		      OR (slot_state = 'DEFERRED' AND retry_not_before <= clock_timestamp())
		      OR (slot_state = 'ACTIVE' AND lease_expires_at <= clock_timestamp())
		  )
		  AND (owner_instance IS NULL OR lease_expires_at <= clock_timestamp())
		RETURNING job_key, collection_job_kind, owner_instance,
		          fence_epoch, projection_generation, scheduled_for
	`, spec.JobKey, owner, generation, spec.PollInterval.Milliseconds(), r.config.LeaseTTL.Milliseconds()).Scan(
		&proof.JobKey, &proof.CollectionJobKind, &proof.OwnerInstance,
		&proof.FenceEpoch, &proof.ProjectionGeneration, &proof.ScheduledFor,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return contract.LeaseProof{}, ErrNotAcquired
	}
	if err != nil {
		return contract.LeaseProof{}, fmt.Errorf("acquire collection job lease: update job row: %w", err)
	}
	proof.ScheduledFor = proof.ScheduledFor.UTC()
	return proof, nil
}

type JobLease struct {
	repository *Repository
	spec       JobSpec
	proof      contract.LeaseProof
}

func (l *JobLease) Proof() contract.LeaseProof {
	if l == nil {
		return contract.LeaseProof{}
	}
	return l.proof
}

func (l *JobLease) Renew(ctx context.Context) error {
	if l == nil || l.repository == nil {
		return fmt.Errorf("renew collection job lease: %w", ErrFenceLost)
	}
	var jobKey string
	err := l.repository.pool.QueryRow(ctx, `
		UPDATE youtube_collection_job_leases AS job
		SET lease_expires_at = clock_timestamp() + ($6::bigint * INTERVAL '1 millisecond'),
		    updated_at = clock_timestamp()
		WHERE job.job_key = $1
		  AND job.owner_instance = $2
		  AND job.fence_epoch = $3
		  AND job.projection_generation = $4
		  AND job.scheduled_for = $5
		  AND job.slot_state = 'ACTIVE'
		  AND job.lease_expires_at > clock_timestamp()
		  AND EXISTS (
		      SELECT 1
		      FROM youtube_collection_projection_generations AS generation
		      WHERE generation.generation = job.projection_generation
		        AND generation.status = 'CURRENT'
		        AND generation.valid_until > clock_timestamp()
		  )
		RETURNING job.job_key
	`, l.proof.JobKey, l.proof.OwnerInstance, l.proof.FenceEpoch,
		l.proof.ProjectionGeneration, l.proof.ScheduledFor, l.repository.config.LeaseTTL.Milliseconds()).Scan(&jobKey)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrFenceLost
	}
	if err != nil {
		return fmt.Errorf("renew collection job lease: %w", err)
	}
	return nil
}

func (l *JobLease) Complete(ctx context.Context) error {
	return l.finish(ctx, "", time.Time{}, "complete")
}

func (l *JobLease) Defer(ctx context.Context, retryAt time.Time, code string) error {
	code = strings.TrimSpace(code)
	if retryAt.IsZero() || code == "" || len(code) > 128 {
		return fmt.Errorf("defer collection job lease: %w", ErrInvalidJob)
	}
	return l.finish(ctx, code, retryAt.UTC(), "defer")
}

func (l *JobLease) Release(ctx context.Context) error {
	if l == nil || l.repository == nil {
		return fmt.Errorf("release collection job lease: %w", ErrFenceLost)
	}
	delay := deterministicJitter(l.proof, l.repository.config.MinReleaseJitter, l.repository.config.MaxReleaseJitter)
	var jobKey string
	err := l.repository.pool.QueryRow(ctx, `
		UPDATE youtube_collection_job_leases
		SET slot_state = 'DEFERRED',
		    owner_instance = NULL,
		    lease_expires_at = NULL,
		    retry_not_before = clock_timestamp() + ($6::bigint * INTERVAL '1 millisecond'),
		    last_error_code = 'shutdown_release',
		    updated_at = clock_timestamp()
		WHERE job_key = $1 AND owner_instance = $2 AND fence_epoch = $3
		  AND projection_generation = $4 AND scheduled_for = $5
		  AND slot_state = 'ACTIVE' AND lease_expires_at > clock_timestamp()
		RETURNING job_key
	`, l.proof.JobKey, l.proof.OwnerInstance, l.proof.FenceEpoch,
		l.proof.ProjectionGeneration, l.proof.ScheduledFor, delay.Milliseconds()).Scan(&jobKey)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrFenceLost
	}
	if err != nil {
		return fmt.Errorf("release collection job lease: %w", err)
	}
	return nil
}

func (l *JobLease) finish(ctx context.Context, code string, retryAt time.Time, action string) error {
	if l == nil || l.repository == nil {
		return fmt.Errorf("%s collection job lease: %w", action, ErrFenceLost)
	}
	var jobKey string
	var err error
	if action == "complete" {
		err = l.repository.pool.QueryRow(ctx, `
			UPDATE youtube_collection_job_leases
			SET slot_state = 'IDLE',
			    owner_instance = NULL,
			    lease_expires_at = NULL,
			    retry_not_before = NULL,
			    last_completed_at = clock_timestamp(),
			    last_error_code = NULL,
			    next_due_at = scheduled_for + (poll_interval_ms * INTERVAL '1 millisecond'),
			    updated_at = clock_timestamp()
			WHERE job_key = $1 AND owner_instance = $2 AND fence_epoch = $3
			  AND projection_generation = $4 AND scheduled_for = $5
			  AND slot_state = 'ACTIVE' AND lease_expires_at > clock_timestamp()
			RETURNING job_key
		`, l.proof.JobKey, l.proof.OwnerInstance, l.proof.FenceEpoch,
			l.proof.ProjectionGeneration, l.proof.ScheduledFor).Scan(&jobKey)
	} else {
		minDelay := l.repository.config.MinRetryDelay
		maxDelay := l.repository.config.MaxRetryDelay
		err = l.repository.pool.QueryRow(ctx, `
			UPDATE youtube_collection_job_leases
			SET slot_state = 'DEFERRED',
			    owner_instance = NULL,
			    lease_expires_at = NULL,
			    retry_not_before = $6,
			    last_error_code = $7,
			    updated_at = clock_timestamp()
			WHERE job_key = $1 AND owner_instance = $2 AND fence_epoch = $3
			  AND projection_generation = $4 AND scheduled_for = $5
			  AND slot_state = 'ACTIVE' AND lease_expires_at > clock_timestamp()
			  AND $6 >= clock_timestamp() + ($8::bigint * INTERVAL '1 millisecond')
			  AND $6 <= clock_timestamp() + ($9::bigint * INTERVAL '1 millisecond')
			RETURNING job_key
		`, l.proof.JobKey, l.proof.OwnerInstance, l.proof.FenceEpoch,
			l.proof.ProjectionGeneration, l.proof.ScheduledFor, retryAt, code,
			minDelay.Milliseconds(), maxDelay.Milliseconds()).Scan(&jobKey)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrFenceLost
	}
	if err != nil {
		return fmt.Errorf("%s collection job lease: %w", action, err)
	}
	return nil
}

func deterministicJitter(proof contract.LeaseProof, minimum, maximum time.Duration) time.Duration {
	if maximum <= minimum {
		return minimum
	}
	hash := fnv.New64a()
	_, _ = hash.Write([]byte(fmt.Sprintf("%s\x00%d", proof.JobKey, proof.FenceEpoch)))
	span := uint64(maximum - minimum)
	return minimum + time.Duration(hash.Sum64()%(span+1))
}

func emissionKinds(definition sourceobservation.JobContract, provider contract.Provider) []contract.ObservationKind {
	kinds := make([]contract.ObservationKind, 0, len(definition.Emissions))
	for _, emission := range definition.Emissions {
		if emission.Provider == provider {
			kinds = append(kinds, emission.Kind)
		}
	}
	return kinds
}
