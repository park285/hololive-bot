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

	scope := collection.MembershipScopeFor(definition)

	proof, err := dbx.InPgxTxWithResult(ctx, r.pool, func(tx dbx.Tx) (contract.LeaseProof, error) {
		return r.acquireTx(ctx, tx, spec, owner, kinds, scope)
	})
	if err != nil {
		return nil, fmt.Errorf("in pgx tx with result: %w", err)
	}

	return &JobLease{repository: r, spec: *spec, contract: definition, scope: scope, proof: proof}, nil
}

// acquireTx의 잠금 순서는 projection guard(share) → lease 행(SKIP LOCKED)이며 publish·CompleteCurrent와 같다.
// Proof의 projection generation은 획득 시점 CURRENT이고 lease 수명 동안 바뀌지 않는다. 이후 유효성은
// 전역 generation 동일성이 아니라 lease에 기록한 범위·행 수와 target의 member_since_generation으로 판정한다.
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

	memberCount, err := verifyAcquireTargets(ctx, tx, spec, kinds, scope, generation)
	if err != nil {
		return contract.LeaseProof{}, fmt.Errorf("verify acquire targets: %w", err)
	}

	if insertErr := insertAcquireJobRow(ctx, tx, spec, generation); insertErr != nil {
		return contract.LeaseProof{}, fmt.Errorf("insert acquire job row: %w", insertErr)
	}

	proof, identity, err := acquireLeaseProof(ctx, tx, spec, owner, generation, r.config.LeaseTTL, scope, memberCount)
	if err != nil {
		return contract.LeaseProof{}, fmt.Errorf("acquire lease proof: %w", err)
	}

	if err := verifyAcquireJobIdentity(spec, &proof, identity); err != nil {
		return contract.LeaseProof{}, fmt.Errorf("verify acquire job identity: %w", err)
	}

	return proof, nil
}

// lockProjectionGuard는 API refresh와 공유하는 guard를 share 잠금하고 잠금 이후 snapshot의 CURRENT를 읽는다.
// CURRENT가 없어도 guard는 잠기며 current=false를 반환한다. 소유 손실 판정을 앞세울 호출자가 이어서 lease를 확인한다.
func lockProjectionGuard(ctx context.Context, tx dbx.Tx) (int64, bool, error) {
	var generation int64

	err := tx.QueryRow(ctx, sqlProjectionLock).Scan(&generation)

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

// verifyAcquireTargets는 cadence bundle의 주기·신규 입장 가능 여부를 확인하고 lease에 기록할 membership 행 수를 반환한다.
// 현재 generation보다 큰 member_since_generation이나 그 값이 없는 행이 있으면 범위를 증명할 수 없으므로 획득하지 않는다.
func verifyAcquireTargets(
	ctx context.Context,
	tx dbx.Tx,
	spec *JobSpec,
	kinds []contract.ObservationKind,
	scope collection.MembershipScope,
	generation int64,
) (int32, error) {
	kindValues := make([]string, len(kinds))
	for i := range kinds {
		kindValues[i] = string(kinds[i])
	}

	var (
		bundle  acquireTargetBundle
		current bool
	)

	err := tx.QueryRow(
		ctx,
		sqlTargetBundle,
		generation,
		kindValues,
		scope.ExactSubject,
		spec.SubjectKey,
		scope.Kinds,
	).Scan(&bundle.targetCount, &bundle.minIntervalMS, &bundle.maxIntervalMS, &bundle.admissibleCount, &bundle.memberCount, &bundle.membersProven, &current)
	if err != nil {
		return 0, fmt.Errorf("acquire collection job lease: verify target set: %w", err)
	}

	if !current {
		return 0, collection.ErrProjectionStale
	}

	return bundle.admit(spec)
}

func (b *acquireTargetBundle) admit(spec *JobSpec) (int32, error) {
	if b.targetCount == 0 {
		return 0, collection.ErrTargetDisabled
	}

	if acquireCadenceMismatch(spec, b.minIntervalMS, b.maxIntervalMS) {
		return 0, fmt.Errorf("acquire collection job lease: %w: target cadence does not match job", ErrInvalidJob)
	}

	// 다음 확인 시각 전인 대상만 남았으면 지금은 입장시키지 않는다. 이미 획득한 작업에는 적용하지 않는다.
	if b.admissibleCount == 0 {
		return 0, ErrNotAcquired
	}

	// 범위 판정과 int32 변환이 같은 지역 값을 써야 행 수가 lease 열 범위 안에 있음을 증명할 수 있다.
	memberCount := b.memberCount
	if !b.membersProven || memberCount < b.targetCount || memberCount < 0 || memberCount > math.MaxInt32 {
		return 0, fmt.Errorf("acquire collection job lease: %w: current target membership is unproven", ErrInvalidJob)
	}

	return int32(memberCount), nil
}

func acquireCadenceMismatch(spec *JobSpec, minIntervalMS, maxIntervalMS int64) bool {
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
	scope collection.MembershipScope,
	memberCount int32,
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
		scope.Kinds,
		scope.ExactSubject,
		memberCount,
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
	scope      collection.MembershipScope
	proof      contract.LeaseProof
}

func (l *JobLease) Proof() contract.LeaseProof {
	if l == nil {
		return contract.LeaseProof{}
	}

	return l.proof
}

// Renew는 소유 증명과 job membership을 함께 판정한다. 소유를 잃었으면 ErrFenceLost를 반환하고,
// 소유는 유지되지만 범위가 무효가 되었으면 lease를 연장하지 않고 ErrProjectionStale 또는 ErrTargetDisabled를 반환한다.
// 후자의 경우 호출자는 callback을 취소·join한 뒤 ReleaseSuperseded로 fenced release한다.
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

// renewTx는 lease 행만 잠근다(guard 없음). 잠금 이후 별도 문장으로 membership을 평가해 publish 종료를 기다린 뒤에도
// 새 snapshot으로 판정한다.
func (l *JobLease) renewTx(ctx context.Context, tx dbx.Tx) error {
	if err := lockActiveLease(ctx, tx, &l.proof); err != nil {
		return fmt.Errorf("lock active lease: %w", err)
	}

	if err := collection.VerifyLeaseMembership(ctx, tx, &l.proof, l.scope); err != nil {
		return fmt.Errorf("verify lease membership: %w", err)
	}

	var jobKey string

	err := tx.QueryRow(ctx, sqlLeaseRenew, l.proof.JobKey, l.proof.OwnerInstance, l.proof.FenceEpoch,
		l.proof.ProjectionGeneration, l.proof.ScheduledFor, l.repository.config.LeaseTTL.Milliseconds()).Scan(&jobKey)

	if errors.Is(err, pgx.ErrNoRows) {
		return collection.ErrFenceLost
	}

	if err != nil {
		return fmt.Errorf("extend lease: %w", err)
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
