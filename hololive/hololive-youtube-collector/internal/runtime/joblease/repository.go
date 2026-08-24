package joblease

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/dbx"
	"github.com/kapu/hololive-shared/pkg/service/youtube/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
)

type Repository struct {
	pool      *pgxpool.Pool
	config    Config
	contracts sourceobservation.JobContractSet
}

func NewRepository(pool *pgxpool.Pool, config *Config) (*Repository, error) {
	if pool == nil {
		return nil, errors.New("create collection job lease repository: pool is nil")
	}

	if config == nil {
		return nil, errors.New("create collection job lease repository: config is nil")
	}

	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("validate: %w", err)
	}

	return &Repository{pool: pool, config: *config, contracts: sourceobservation.InitialJobContracts()}, nil
}

func (r *Repository) Acquire(ctx context.Context, spec *JobSpec, owner string) (*JobLease, error) {
	if r == nil || r.pool == nil || r.contracts == nil {
		return nil, errors.New("acquire collection job lease: repository is not configured")
	}

	if spec == nil {
		return nil, fmt.Errorf("acquire collection job lease: %w: spec is nil", ErrInvalidJob)
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
		return nil, fmt.Errorf("in pgx tx with result: %w", err)
	}

	return &JobLease{repository: r, spec: *spec, contract: definition.Clone(), proof: proof}, nil
}

func (r *Repository) acquireTx(
	ctx context.Context,
	tx dbx.Tx,
	spec *JobSpec,
	owner string,
	definition sourceobservation.JobContract,
	kinds []contract.ObservationKind,
) (contract.LeaseProof, error) {
	generation, err := lockAcquireProjection(ctx, tx)
	if err != nil {
		return contract.LeaseProof{}, fmt.Errorf("lock acquire projection: %w", err)
	}

	if verifyErr := r.verifyAcquireTargets(ctx, tx, spec, definition, kinds, generation); verifyErr != nil {
		return contract.LeaseProof{}, fmt.Errorf("verify acquire targets: %w", verifyErr)
	}

	if insertErr := insertAcquireJobRow(ctx, tx, spec, generation); insertErr != nil {
		return contract.LeaseProof{}, fmt.Errorf("insert acquire job row: %w", insertErr)
	}

	proof, err := acquireLeaseProof(ctx, tx, spec, owner, generation, r.config.LeaseTTL)
	if err != nil {
		return contract.LeaseProof{}, fmt.Errorf("acquire lease proof: %w", err)
	}

	if err := verifyAcquireJobIdentity(ctx, tx, spec); err != nil {
		return contract.LeaseProof{}, fmt.Errorf("verify acquire job identity: %w", err)
	}

	return proof, nil
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
	spec *JobSpec,
	definition sourceobservation.JobContract,
	kinds []contract.ObservationKind,
	generation int64,
) error {
	kindValues := make([]string, len(kinds))
	for i := range kinds {
		kindValues[i] = string(kinds[i])
	}

	var (
		targetCount   int
		minIntervalMS int64
		maxIntervalMS int64
	)

	exact := definition.Membership() == sourceobservation.JobMembershipExactSubject

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

func acquireCadenceMismatch(spec *JobSpec, _ sourceobservation.JobContract, minIntervalMS, maxIntervalMS int64) bool {
	return minIntervalMS <= 0 || minIntervalMS != maxIntervalMS || minIntervalMS != spec.PollInterval.Milliseconds()
}

func insertAcquireJobRow(ctx context.Context, tx dbx.Tx, spec *JobSpec, generation int64) error {
	if _, err := tx.Exec(ctx, mustSQL("repository_lease_insert_0144_06.sql"), spec.JobKey, spec.Provider, spec.Class, spec.CollectionJobKind, spec.SubjectKey,
		generation, spec.PollInterval.Milliseconds()); err != nil {
		return fmt.Errorf("acquire collection job lease: create job row: %w", err)
	}

	return nil
}

// 0144_07은 job_key만 qual로 쓰는 무가드 FOR UPDATE다. 자기 방어적인 0144_08 UPDATE가 성공해
// 행 배타 락을 이미 쥔 뒤에만 호출해야 다른 트랜잭션의 행 락 뒤로 직렬화되지 않는다.
func verifyAcquireJobIdentity(ctx context.Context, tx dbx.Tx, spec *JobSpec) error {
	var (
		storedProvider string
		storedClass    string
		storedKind     string
		storedSubject  string
	)

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
	spec *JobSpec,
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
	contract   sourceobservation.JobContract
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
	if err := l.finish(ctx, "", time.Time{}, "", "", "complete"); err != nil {
		return fmt.Errorf("finish: %w", err)
	}

	return nil
}

func (l *JobLease) Defer(ctx context.Context, retryAt time.Time, code, class, detail string) error {
	code = strings.TrimSpace(code)
	class = strings.TrimSpace(class)

	if retryAt.IsZero() || !validDeferFailureTuple(code, class) || invalidDeferDetail(detail) {
		return fmt.Errorf("defer collection job lease: %w", ErrInvalidJob)
	}

	detail = collecterr.SanitizeDetail(detail)
	if strings.TrimSpace(detail) == "" {
		detail = code
	}

	if err := l.finish(ctx, code, retryAt.UTC(), class, detail, "defer"); err != nil {
		return fmt.Errorf("finish: %w", err)
	}

	return nil
}

func validDeferFailureTuple(code, class string) bool {
	typed := contract.CollectionErrorCode(code)
	return contract.ValidDurableFailureTuple(typed, contract.FailureClass(class)) && typed.Deferable()
}

func invalidDeferDetail(detail string) bool {
	return len(detail) > collecterr.MaxDetailBytes || !utf8.ValidString(detail) || strings.IndexByte(detail, 0) >= 0
}

func (l *JobLease) Release(ctx context.Context, reason ReleaseReason) error {
	if !reason.Valid() {
		return fmt.Errorf("release collection job lease: %w", ErrInvalidJob)
	}

	if l == nil || l.repository == nil {
		return fmt.Errorf("release collection job lease: %w", ErrFenceLost)
	}

	delay := deterministicJitter(&l.proof, l.repository.config.MinReleaseJitter, l.repository.config.MaxReleaseJitter)

	if reason == ReleaseSuperseded {
		delay = 0
	}

	if err := dbx.InPgxTx(ctx, l.repository.pool, func(tx dbx.Tx) error {
		return releaseLeaseTx(ctx, tx, &l.proof, reason, delay)
	}); err != nil {
		return fmt.Errorf("in pgx tx: %w", err)
	}

	return nil
}
