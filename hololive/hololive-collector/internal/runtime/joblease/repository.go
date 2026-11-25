package joblease

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"math"
	"math/big"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/park285/shared-go/postgres/pgxdb"

	"github.com/kapu/hololive-collector/internal/runtime/collecterr"
	"github.com/kapu/hololive-collector/internal/runtime/collection"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/dbx"
)

var canonicalJobContracts = collection.InitialJobContracts()

type Repository struct {
	pool      *pgxpool.Pool
	config    Config
	contracts collection.StaticJobContracts
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

	return &Repository{pool: pool, config: *config, contracts: canonicalJobContracts}, nil
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

	scope := collection.MembershipScopeFor(definition)

	proof, err := dbx.InPgxTxWithResult(ctx, r.pool, func(tx dbx.Tx) (contract.LeaseProof, error) {
		return r.acquireTx(ctx, tx, spec, owner, kinds, scope)
	})
	if err != nil {
		return nil, fmt.Errorf("acquire collection job lease: %w", err)
	}

	return &JobLease{repository: r, spec: *spec, contract: definition, scope: scope, proof: proof}, nil
}

func (r *Repository) acquireTx(
	ctx context.Context,
	tx dbx.Tx,
	spec *JobSpec,
	owner string,
	kinds []contract.ObservationKind,
	scope collection.MembershipScope,
) (contract.LeaseProof, error) {
	generation, current, err := lockProjectionGuard(ctx, tx)
	if err != nil {
		return contract.LeaseProof{}, fmt.Errorf("lock projection guard: %w", err)
	}

	if !current {
		return contract.LeaseProof{}, collection.ErrProjectionStale
	}

	proof, identity, err := acquireLeaseProof(ctx, tx, spec, owner, generation, r.config.LeaseTTL, kinds, scope)
	if err != nil {
		return contract.LeaseProof{}, fmt.Errorf("acquire lease proof: %w", err)
	}

	if err := verifyAcquireJobIdentity(spec, &proof, identity); err != nil {
		return contract.LeaseProof{}, fmt.Errorf("verify acquire job identity: %w", err)
	}

	return proof, nil
}

func lockProjectionGuard(ctx context.Context, tx dbx.Tx) (int64, bool, error) {
	return scanProjectionGuard(pgxdb.QueryRow(ctx, tx, lockProjectionGuardShareSQL))
}

func scanProjectionGuard(row pgx.Row) (int64, bool, error) {
	var generation int64

	err := row.Scan(&generation)

	if errors.Is(err, pgx.ErrNoRows) {
		return 0, false, nil
	}

	if err != nil {
		return 0, false, fmt.Errorf("lock current collection projection: %w", err)
	}

	return generation, true, nil
}

type acquireTargetBundle struct {
	targetCount     int64
	minIntervalMS   int64
	maxIntervalMS   int64
	admissibleCount int64
	memberCount     int64
	membersProven   bool
}

func (b *acquireTargetBundle) admit(spec *JobSpec) (int32, error) {
	if b.targetCount == 0 {
		return 0, collection.ErrTargetDisabled
	}

	if acquireCadenceMismatch(spec, b.minIntervalMS, b.maxIntervalMS) {
		return 0, fmt.Errorf("acquire collection job lease: %w: target cadence does not match job", ErrInvalidJob)
	}

	if b.admissibleCount == 0 {
		return 0, ErrNotAcquired
	}

	memberCount := b.memberCount
	if !b.membersProven || memberCount < b.targetCount || memberCount < 0 || memberCount > math.MaxInt32 {
		return 0, fmt.Errorf("acquire collection job lease: %w: current target membership is unproven", ErrInvalidJob)
	}

	return int32(memberCount), nil
}

func acquireCadenceMismatch(spec *JobSpec, minIntervalMS, maxIntervalMS int64) bool {
	return minIntervalMS <= 0 || minIntervalMS != maxIntervalMS || minIntervalMS != spec.PollInterval.Milliseconds()
}

type acquiredJobIdentity struct {
	provider   string
	class      string
	subjectKey string
}

func verifyAcquireJobIdentity(spec *JobSpec, proof *contract.LeaseProof, identity acquiredJobIdentity) error {
	if identity.provider != string(spec.Provider) || identity.class != spec.Class ||
		proof.CollectionJobKind != spec.CollectionJobKind || identity.subjectKey != spec.SubjectKey {
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
	kinds []contract.ObservationKind,
	scope collection.MembershipScope,
) (contract.LeaseProof, acquiredJobIdentity, error) {
	kindValues := make([]string, len(kinds))
	for i := range kinds {
		kindValues[i] = string(kinds[i])
	}

	query := repositoryLeaseAcquireSQL

	if scope.ExactSubject {
		query = repositoryLeaseAcquireExactSQL
	}

	var (
		bundle                                                   acquireTargetBundle
		current, created                                         bool
		jobKey, provider, class, jobKind, subject, acquiredOwner pgtype.Text
		epoch, projection                                        pgtype.Int8
		scheduled                                                pgtype.Timestamptz
	)

	err := pgxdb.QueryRow(ctx, tx, query,
		generation, kindValues, scope.Kinds, spec.SubjectKey,
		spec.JobKey, owner, spec.PollInterval.Milliseconds(), leaseTTL.Milliseconds(),
		scope.ExactSubject, spec.Provider, spec.Class, spec.CollectionJobKind,
	).Scan(
		&bundle.targetCount, &bundle.minIntervalMS, &bundle.maxIntervalMS,
		&bundle.admissibleCount, &bundle.memberCount, &bundle.membersProven, &current,
		&jobKey, &provider, &class, &jobKind, &subject, &acquiredOwner, &epoch, &projection, &scheduled,
		&created,
	)
	if err != nil {
		return contract.LeaseProof{}, acquiredJobIdentity{}, fmt.Errorf("acquire collection job lease: admit and acquire job row: %w", err)
	}

	if err := validateAcquisition(spec, &bundle, current, jobKey.Valid); err != nil {
		return contract.LeaseProof{}, acquiredJobIdentity{}, err
	}

	proof := contract.LeaseProof{
		JobKey: jobKey.String, CollectionJobKind: jobKind.String,
		OwnerInstance: acquiredOwner.String, FenceEpoch: epoch.Int64,
		ProjectionGeneration: projection.Int64, ScheduledFor: scheduled.Time,
	}
	identity := acquiredJobIdentity{provider: provider.String, class: class.String, subjectKey: subject.String}

	if created {
		if err := pgxdb.QueryRow(ctx, tx, repositoryLeaseInitializeSQL,
			proof.JobKey, proof.OwnerInstance, proof.FenceEpoch, proof.ProjectionGeneration, leaseTTL.Milliseconds(),
		).Scan(&proof.ScheduledFor); err != nil {
			return contract.LeaseProof{}, acquiredJobIdentity{}, fmt.Errorf("acquire collection job lease: initialize created lease: %w", err)
		}
	}

	proof.ScheduledFor = proof.ScheduledFor.UTC()

	return proof, identity, nil
}

func validateAcquisition(spec *JobSpec, bundle *acquireTargetBundle, current, acquired bool) error {
	if !current {
		return collection.ErrProjectionStale
	}

	if _, err := bundle.admit(spec); err != nil {
		return fmt.Errorf("verify acquire targets: %w", err)
	}

	if !acquired {
		return ErrNotAcquired
	}

	return nil
}

type JobLease struct {
	repository *Repository
	spec       JobSpec
	contract   collection.JobContract
	scope      collection.MembershipScope
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
		return fmt.Errorf("renew collection job lease: %w", collection.ErrFenceLost)
	}

	if err := dbx.InPgxTx(ctx, l.repository.pool, func(tx dbx.Tx) error {
		return l.renewTx(ctx, tx)
	}); err != nil {
		return fmt.Errorf("renew collection job lease: %w", err)
	}

	return nil
}

func (l *JobLease) renewTx(ctx context.Context, tx dbx.Tx) error {
	if err := lockActiveLease(ctx, tx, &l.proof); err != nil {
		return fmt.Errorf("lock active lease: %w", err)
	}

	if err := verifyLeaseMembership(ctx, tx, &l.proof, l.scope); err != nil {
		return fmt.Errorf("verify lease membership: %w", err)
	}

	var jobKey string

	err := pgxdb.QueryRow(ctx, tx, renewTxSQL, l.proof.JobKey, l.proof.OwnerInstance, l.proof.FenceEpoch,
		l.proof.ProjectionGeneration, l.proof.ScheduledFor, l.repository.config.LeaseTTL.Milliseconds()).Scan(&jobKey)

	if errors.Is(err, pgx.ErrNoRows) {
		return collection.ErrFenceLost
	}

	if err != nil {
		return fmt.Errorf("extend lease: %w", err)
	}

	return nil
}

func (l *JobLease) Defer(ctx context.Context, input collection.DeferCollectionInput) error {
	var (
		q     pgxdb.Querier
		proof *contract.LeaseProof
	)

	if l != nil && l.repository != nil {
		q, proof = l.repository.pool, &l.proof
	}

	return DeferLease(ctx, q, proof, input)
}

func DeferLease(ctx context.Context, q pgxdb.Querier, proof *contract.LeaseProof, input collection.DeferCollectionInput) error {
	if err := input.Validate(); err != nil {
		return fmt.Errorf("defer collection job lease: %w: %w", ErrInvalidJob, err)
	}

	if q == nil || proof == nil {
		return fmt.Errorf("defer collection job lease: %w", collection.ErrFenceLost)
	}

	diagnostic := input.Diagnostic()
	code := string(diagnostic.Code())

	detail := collecterr.SanitizeDetail(diagnostic.Detail())
	if strings.TrimSpace(detail) == "" {
		detail = code
	}

	bounds := input.Bounds()

	var jobKey string

	err := pgxdb.QueryRow(ctx, q, deferSQL, proof.JobKey, proof.OwnerInstance, proof.FenceEpoch,
		proof.ProjectionGeneration, proof.ScheduledFor, input.Schedule().At(), code, string(diagnostic.Class()), detail,
		bounds.Minimum.Milliseconds(), bounds.Maximum.Milliseconds(), input.Schedule().IsNotBefore()).Scan(&jobKey)

	if errors.Is(err, pgx.ErrNoRows) {
		return collection.ErrFenceLost
	}

	if err != nil {
		return fmt.Errorf("defer collection job lease: %w", err)
	}

	return nil
}

func (l *JobLease) Release(ctx context.Context, reason ReleaseReason) error {
	if !reason.Valid() {
		return fmt.Errorf("release collection job lease: %w", ErrInvalidJob)
	}

	if l == nil || l.repository == nil {
		return fmt.Errorf("release collection job lease: %w", collection.ErrFenceLost)
	}

	var (
		jobKey string
		err    error
	)

	if reason == ReleaseSuperseded {
		err = pgxdb.QueryRow(ctx, l.repository.pool, repositoryLeaseReleaseSupersededSQL,
			l.proof.JobKey, l.proof.OwnerInstance, l.proof.FenceEpoch, l.proof.ProjectionGeneration, l.proof.ScheduledFor,
			string(reason.ErrorCode()),
		).Scan(&jobKey)
	} else {
		delay := deterministicJitter(&l.proof, l.repository.config.MinReleaseJitter, l.repository.config.MaxReleaseJitter)

		err = pgxdb.QueryRow(ctx, l.repository.pool, releaseSQL,
			l.proof.JobKey, l.proof.OwnerInstance, l.proof.FenceEpoch, l.proof.ProjectionGeneration, l.proof.ScheduledFor,
			delay.Milliseconds(), string(reason.ErrorCode()),
		).Scan(&jobKey)
	}

	if errors.Is(err, pgx.ErrNoRows) {
		return collection.ErrFenceLost
	}

	if err != nil {
		return fmt.Errorf("release collection job lease: %w", err)
	}

	return nil
}

func deterministicJitter(proof *contract.LeaseProof, minimum, maximum time.Duration) time.Duration {
	if maximum <= minimum {
		return minimum
	}

	hash := fnv.New64a()

	_, _ = hash.Write(fmt.Appendf(nil, "%s\x00%d", proof.JobKey, proof.FenceEpoch))

	span := big.NewInt((maximum - minimum).Nanoseconds() + 1)
	offset := new(big.Int).SetUint64(hash.Sum64())
	offset.Mod(offset, span)

	return minimum + time.Duration(offset.Int64())
}
