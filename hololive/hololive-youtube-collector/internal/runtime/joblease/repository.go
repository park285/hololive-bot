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
	definition, kinds, err := r.candidateDefinition(provider, jobKind, limit)
	if err != nil {
		return nil, err
	}
	generation, err := r.currentProjectionGeneration(ctx)
	if err != nil {
		return nil, err
	}
	kindValues := make([]string, len(kinds))
	for i := range kinds {
		kindValues[i] = string(kinds[i])
	}
	if definition.Class == "GLOBAL" {
		return r.globalCandidate(ctx, provider, jobKind, generation, kindValues, definition)
	}
	return r.scanTargetCandidates(ctx, provider, jobKind, generation, kindValues, definition.Class, limit)
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
	generation, err := lockAcquireProjection(ctx, tx)
	if err != nil {
		return contract.LeaseProof{}, err
	}
	if err := r.verifyAcquireTargets(ctx, tx, spec, definition, kinds, generation); err != nil {
		return contract.LeaseProof{}, err
	}
	if err := ensureAcquireJobRow(ctx, tx, spec, generation); err != nil {
		return contract.LeaseProof{}, err
	}
	return acquireLeaseProof(ctx, tx, spec, owner, generation, r.config.LeaseTTL)
}

func lockAcquireProjection(ctx context.Context, tx dbx.Tx) (int64, error) {
	var generation int64
	err := tx.QueryRow(ctx, mustSQL("repository_projection_lock_0144_05.sql")).Scan(&generation)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrProjectionStale
	}
	if err != nil {
		return 0, fmt.Errorf("acquire collection job lease: lock current projection: %w", err)
	}
	return generation, nil
}

func (r *Repository) verifyAcquireTargets(
	ctx context.Context,
	tx dbx.Tx,
	spec JobSpec,
	definition sourceobservation.JobContract,
	kinds []contract.ObservationKind,
	generation int64,
) error {
	kindValues := make([]string, len(kinds))
	for i := range kinds {
		kindValues[i] = string(kinds[i])
	}
	var targetCount int
	var minIntervalMS int64
	var maxIntervalMS int64
	exact := definition.Membership == sourceobservation.JobMembershipExactSubject
	err := tx.QueryRow(
		ctx,
		mustSQL("repository_target_bundle_0144_04.sql"),
		generation,
		kindValues,
		exact,
		spec.SubjectKey,
	).Scan(&targetCount, &minIntervalMS, &maxIntervalMS)
	if err != nil {
		return fmt.Errorf("acquire collection job lease: verify target set: %w", err)
	}
	if targetCount == 0 {
		return ErrTargetDisabled
	}
	if acquireCadenceMismatch(spec, definition, minIntervalMS, maxIntervalMS) {
		return fmt.Errorf("acquire collection job lease: %w: target cadence does not match job", ErrInvalidJob)
	}
	return nil
}

func acquireCadenceMismatch(spec JobSpec, _ sourceobservation.JobContract, minIntervalMS, maxIntervalMS int64) bool {
	return minIntervalMS <= 0 || minIntervalMS != maxIntervalMS || minIntervalMS != spec.PollInterval.Milliseconds()
}

func ensureAcquireJobRow(ctx context.Context, tx dbx.Tx, spec JobSpec, generation int64) error {
	if _, err := tx.Exec(ctx, mustSQL("repository_lease_insert_0144_06.sql"), spec.JobKey, spec.Provider, spec.Class, spec.CollectionJobKind, spec.SubjectKey,
		generation, spec.PollInterval.Milliseconds()); err != nil {
		return fmt.Errorf("acquire collection job lease: create job row: %w", err)
	}
	var storedProvider string
	var storedClass string
	var storedKind string
	var storedSubject string
	err := tx.QueryRow(ctx, mustSQL("repository_lease_lock_0144_07.sql"), spec.JobKey).
		Scan(&storedProvider, &storedClass, &storedKind, &storedSubject)
	if err != nil {
		return fmt.Errorf("acquire collection job lease: lock job row: %w", err)
	}
	if storedProvider != string(spec.Provider) || storedClass != spec.Class || storedKind != spec.CollectionJobKind || storedSubject != spec.SubjectKey {
		return fmt.Errorf("acquire collection job lease: %w: job key is bound to another identity", ErrInvalidJob)
	}
	return nil
}

func acquireLeaseProof(
	ctx context.Context,
	tx dbx.Tx,
	spec JobSpec,
	owner string,
	generation int64,
	leaseTTL time.Duration,
) (contract.LeaseProof, error) {
	var proof contract.LeaseProof
	err := tx.QueryRow(
		ctx,
		mustSQL("repository_lease_acquire_0144_08.sql"),
		spec.JobKey,
		owner,
		generation,
		spec.PollInterval.Milliseconds(),
		leaseTTL.Milliseconds(),
	).Scan(
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
	err := l.repository.pool.QueryRow(ctx, mustSQL("repository_lease_renew_0144_09.sql"), l.proof.JobKey, l.proof.OwnerInstance, l.proof.FenceEpoch,
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
	err := l.repository.pool.QueryRow(ctx, mustSQL("repository_lease_release_0144_10.sql"), l.proof.JobKey, l.proof.OwnerInstance, l.proof.FenceEpoch,
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
		err = l.repository.pool.QueryRow(ctx, mustSQL("repository_lease_complete_0144_11.sql"), l.proof.JobKey, l.proof.OwnerInstance, l.proof.FenceEpoch,
			l.proof.ProjectionGeneration, l.proof.ScheduledFor).Scan(&jobKey)
	} else {
		minDelay := l.repository.config.MinRetryDelay
		maxDelay := l.repository.config.MaxRetryDelay
		err = l.repository.pool.QueryRow(ctx, mustSQL("repository_lease_defer_0144_12.sql"), l.proof.JobKey, l.proof.OwnerInstance, l.proof.FenceEpoch,
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
