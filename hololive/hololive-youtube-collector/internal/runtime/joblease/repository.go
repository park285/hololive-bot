package joblease

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"math/big"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/dbx"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collection"
)

// canonicalJobContracts는 패키지 초기화 때 한 번 검증된 계약 집합이다. 패키지 안에서 변경하지 않는다.
var canonicalJobContracts = collection.InitialJobContracts()

type Repository struct {
	pool      *pgxpool.Pool
	config    Config
	contracts collection.JobContractSet
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

	proof, err := dbx.InPgxTxWithResult(ctx, r.pool, func(tx dbx.Tx) (contract.LeaseProof, error) {
		return r.acquireTx(ctx, tx, spec, owner, definition, kinds)
	})
	if err != nil {
		return nil, fmt.Errorf("in pgx tx with result: %w", err)
	}

	return &JobLease{repository: r, spec: *spec, contract: definition, proof: proof}, nil
}

func (r *Repository) acquireTx(
	ctx context.Context,
	tx dbx.Tx,
	spec *JobSpec,
	owner string,
	definition collection.JobContract,
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

	proof, identity, err := acquireLeaseProof(ctx, tx, spec, owner, generation, r.config.LeaseTTL)
	if err != nil {
		return contract.LeaseProof{}, fmt.Errorf("acquire lease proof: %w", err)
	}

	if err := verifyAcquireJobIdentity(spec, &proof, identity); err != nil {
		return contract.LeaseProof{}, fmt.Errorf("verify acquire job identity: %w", err)
	}

	return proof, nil
}

func lockAcquireProjection(ctx context.Context, tx dbx.Tx) (int64, error) {
	var generation int64

	err := tx.QueryRow(ctx, sqlProjectionLock).Scan(&generation)

	if errors.Is(err, pgx.ErrNoRows) {
		return 0, collection.ErrProjectionStale
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
	definition collection.JobContract,
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

	exact := definition.Membership() == collection.JobMembershipExactSubject

	err := tx.QueryRow(
		ctx,
		sqlTargetBundle,
		generation,
		kindValues,
		exact,
		spec.SubjectKey,
	).Scan(&targetCount, &minIntervalMS, &maxIntervalMS)
	if err != nil {
		return fmt.Errorf("acquire collection job lease: verify target set: %w", err)
	}

	if targetCount == 0 {
		return collection.ErrTargetDisabled
	}

	if acquireCadenceMismatch(spec, definition, minIntervalMS, maxIntervalMS) {
		return fmt.Errorf("acquire collection job lease: %w: target cadence does not match job", ErrInvalidJob)
	}

	return nil
}

func acquireCadenceMismatch(spec *JobSpec, _ collection.JobContract, minIntervalMS, maxIntervalMS int64) bool {
	return minIntervalMS <= 0 || minIntervalMS != maxIntervalMS || minIntervalMS != spec.PollInterval.Milliseconds()
}

func insertAcquireJobRow(ctx context.Context, tx dbx.Tx, spec *JobSpec, generation int64) error {
	if _, err := tx.Exec(ctx, sqlLeaseInsert, spec.JobKey, spec.Provider, spec.Class, spec.CollectionJobKind, spec.SubjectKey,
		generation, spec.PollInterval.Milliseconds()); err != nil {
		return fmt.Errorf("acquire collection job lease: create job row: %w", err)
	}

	return nil
}

// acquiredJobIdentity는 0144_08 UPDATE가 잠근 행 버전의 식별자를 RETURNING으로 받은 값이다.
type acquiredJobIdentity struct {
	provider   string
	class      string
	subjectKey string
}

// 같은 트랜잭션이 방금 갱신·잠근 행 버전을 다시 읽지 않고 RETURNING 값으로 식별자를 비교한다.
// 이 job_key가 다른 identity에 묶여 있으면 ErrInvalidJob을 반환해 epoch 증가를 포함한 트랜잭션 전체를 되돌린다.
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
) (contract.LeaseProof, acquiredJobIdentity, error) {
	var (
		proof    contract.LeaseProof
		identity acquiredJobIdentity
	)

	err := tx.QueryRow(
		ctx,
		sqlLeaseAcquire,
		spec.JobKey,
		owner,
		generation,
		spec.PollInterval.Milliseconds(),
		leaseTTL.Milliseconds(),
	).Scan(
		&proof.JobKey, &identity.provider, &identity.class, &proof.CollectionJobKind, &identity.subjectKey,
		&proof.OwnerInstance, &proof.FenceEpoch, &proof.ProjectionGeneration, &proof.ScheduledFor,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return contract.LeaseProof{}, acquiredJobIdentity{}, ErrNotAcquired
	}

	if err != nil {
		return contract.LeaseProof{}, acquiredJobIdentity{}, fmt.Errorf("acquire collection job lease: update job row: %w", err)
	}

	proof.ScheduledFor = proof.ScheduledFor.UTC()

	return proof, identity, nil
}

type JobLease struct {
	repository *Repository
	spec       JobSpec
	contract   collection.JobContract
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

	var jobKey string

	err := l.repository.pool.QueryRow(ctx, sqlLeaseRenew, l.proof.JobKey, l.proof.OwnerInstance, l.proof.FenceEpoch,
		l.proof.ProjectionGeneration, l.proof.ScheduledFor, l.repository.config.LeaseTTL.Milliseconds()).Scan(&jobKey)

	if errors.Is(err, pgx.ErrNoRows) {
		return collection.ErrFenceLost
	}

	if err != nil {
		return fmt.Errorf("renew collection job lease: %w", err)
	}

	return nil
}

// Defer는 executor가 만든 typed 재시도 입력으로 lease를 DEFERRED로 전환합니다.
// 공개 진단 생성자는 redaction하지 않으므로 adapter 경계에서 detail을 한 번 더 정제하고,
// retry_not_before는 SQL이 DB 시계 기준 입력 bounds 안으로 보정합니다.
func (l *JobLease) Defer(ctx context.Context, input collection.DeferCollectionInput) error {
	if err := input.Validate(); err != nil {
		return fmt.Errorf("defer collection job lease: %w: %w", ErrInvalidJob, err)
	}

	if l == nil || l.repository == nil {
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

	err := l.repository.pool.QueryRow(ctx, sqlLeaseDefer, l.proof.JobKey, l.proof.OwnerInstance, l.proof.FenceEpoch,
		l.proof.ProjectionGeneration, l.proof.ScheduledFor, input.Schedule().At(), code, string(diagnostic.Class()), detail,
		bounds.Minimum.Milliseconds(), bounds.Maximum.Milliseconds()).Scan(&jobKey)

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

func releaseLeaseTx(
	ctx context.Context,
	tx dbx.Tx,
	proof *contract.LeaseProof,
	reason ReleaseReason,
	delay time.Duration,
) error {
	// release는 last_failure_*를 건드리지 않는다. 177 trigger가 DEFERRED release를 legacy_collector로 덮어쓰던 때는 잠근
	// 사전 값을 되돌렸지만, migration 222가 trigger를 지워 복원 단계도 함께 지웠다(stack-audit 2026-09-26 T17).
	var jobKey string

	err := tx.QueryRow(
		ctx,
		sqlLeaseRelease,
		proof.JobKey, proof.OwnerInstance, proof.FenceEpoch, proof.ProjectionGeneration, proof.ScheduledFor,
		delay.Milliseconds(), string(reason.ErrorCode()),
	).Scan(&jobKey)

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
