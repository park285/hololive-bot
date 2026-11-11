package joblease

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/jackc/pgx/v5"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/dbx"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collection"
)

// Guard를 share 잠금한 뒤 별도 문장으로 소유 증명과 job membership을 판정하므로, API 전환을 기다린 snapshot도 새 CURRENT에서 평가되고 무관한 target 변경으로 거절되지
// 않는다. Membership이 유효하면 CURRENT의 범위는 획득 시점 범위와 같으므로 snapshot의 generation은 lease 증명의 획득 generation으로 표시한다. Not_before 같은
// 입장 조건은 적용하지 않는다.
func (r *Repository) LoadTargetSnapshot(
	ctx context.Context,
	proof *contract.LeaseProof,
	spec *JobSpec,
	job collection.JobContract,
	maxRosterRows int,
) (collection.TargetSnapshot, error) {
	if err := r.validateSnapshotRequest(proof, spec, job, maxRosterRows); err != nil {
		return collection.TargetSnapshot{}, fmt.Errorf("validate snapshot request: %w", err)
	}

	out, err := dbx.InPgxTxWithResult(ctx, r.pool, func(tx dbx.Tx) (collection.TargetSnapshot, error) {
		return loadTargetSnapshotTx(ctx, tx, proof, spec, job, maxRosterRows)
	})
	if err != nil {
		return collection.TargetSnapshot{}, fmt.Errorf("read lease target roster: %w", err)
	}

	return out, nil
}

func loadTargetSnapshotTx(
	ctx context.Context,
	tx dbx.Tx,
	proof *contract.LeaseProof,
	spec *JobSpec,
	job collection.JobContract,
	maxRosterRows int,
) (collection.TargetSnapshot, error) {
	current, err := lockGuardAndVerifyMembership(ctx, tx, proof, collection.MembershipScopeFor(job))
	if err != nil {
		return collection.TargetSnapshot{}, fmt.Errorf("lock guard and verify membership: %w", err)
	}

	requested := job.RequestedKinds()
	kindValues := make([]string, len(requested))

	for index, kind := range requested {
		kindValues[index] = string(kind)
	}

	query := snapshotQuery{current: current, acquired: proof.ProjectionGeneration, requested: requested, kindValues: kindValues, maxRosterRows: maxRosterRows}

	switch job.Membership() {
	case collection.JobMembershipExactSubject:
		out, err := query.loadExact(ctx, tx, spec.SubjectKey)
		if err != nil {
			return out, fmt.Errorf("load exact target snapshot: %w", err)
		}

		return out, nil
	case collection.JobMembershipCurrentProjection:
		out, err := query.loadProjection(ctx, tx)
		if err != nil {
			return out, fmt.Errorf("load projection target snapshot: %w", err)
		}

		return out, nil
	default:
		return collection.TargetSnapshot{}, fmt.Errorf("snapshot invariant: %w", snapshotInvariant("target snapshot membership is invalid"))
	}
}

// snapshotBatchSender는 pgx 트랜잭션의 pipeline 전송 능력이다. Snapshot 트랜잭션은 항상 pgxpool에서 시작하므로
// 이 능력이 없으면 순차 경로로 바꾸지 않고 오류로 드러낸다.
type snapshotBatchSender interface {
	SendBatch(ctx context.Context, batch *pgx.Batch) pgx.BatchResults
}

// lockGuardAndVerifyMembership은 guard share 잠금과 lease membership 판정을 한 왕복으로 보내고 잠금 이후 CURRENT
// generation을 반환한다. 서버는 두 문장을 순서대로 따로 실행하므로 membership은 guard 대기 뒤의 새 snapshot에서
// 평가되고, guard 문장이 실패하면 membership 문장은 실행되지 않는다. CURRENT가 없으면 0을 반환하며 그 판정은
// membership이 소유 손실 다음 순위로 보고한다.
func lockGuardAndVerifyMembership(
	ctx context.Context,
	tx dbx.Tx,
	proof *contract.LeaseProof,
	scope collection.MembershipScope,
) (current int64, err error) {
	sender, ok := tx.(snapshotBatchSender)
	if !ok {
		return 0, errors.New("lock guard and verify membership: transaction does not support pipelined batches")
	}

	membershipQuery, membershipArgs := LeaseMembershipQuery(proof, scope)
	batch := &pgx.Batch{}

	batch.Queue(lockProjectionGuardShareSQL)
	batch.Queue(membershipQuery, membershipArgs...)

	results := sender.SendBatch(ctx, batch)
	if results == nil {
		return 0, errors.New("lock guard and verify membership: batch results are nil")
	}

	// 판정이 앞에서 끝나도 남은 응답을 회수해 연결을 target 조회에 쓸 수 있게 한다.
	defer func() {
		if closeErr := results.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("close snapshot guard batch: %w", closeErr))
		}
	}()

	current, _, err = scanProjectionGuard(results.QueryRow())
	if err != nil {
		return 0, fmt.Errorf("lock projection guard: %w", err)
	}

	if err := ScanLeaseMembership(results.QueryRow()); err != nil {
		return 0, fmt.Errorf("verify lease membership: %w", err)
	}

	return current, nil
}

// snapshotQuery는 guard 아래에서 읽은 CURRENT generation으로 target을 읽고, 결과는 획득 generation으로 식별한다.
type snapshotQuery struct {
	current       int64
	acquired      int64
	requested     []contract.ObservationKind
	kindValues    []string
	maxRosterRows int
}

func (r *Repository) validateSnapshotRequest(
	proof *contract.LeaseProof,
	spec *JobSpec,
	job collection.JobContract,
	maxRosterRows int,
) error {
	if invalidSnapshotRepository(r) || invalidSnapshotProof(proof, spec, maxRosterRows) {
		return fmt.Errorf("snapshot invariant: %w", snapshotInvariant("target snapshot request is invalid"))
	}

	if err := r.validateSnapshotJob(proof, spec, job); err != nil {
		return fmt.Errorf("validate snapshot job: %w", err)
	}

	return nil
}

func invalidSnapshotRepository(r *Repository) bool {
	return r == nil || r.pool == nil || r.contracts == nil
}

func invalidSnapshotProof(proof *contract.LeaseProof, spec *JobSpec, maxRosterRows int) bool {
	if proof == nil || spec == nil || maxRosterRows < 1 {
		return true
	}

	return proof.JobKey != spec.JobKey || proof.CollectionJobKind != spec.CollectionJobKind ||
		proof.ProjectionGeneration <= 0 || proof.FenceEpoch <= 0 || proof.OwnerInstance == "" || proof.ScheduledFor.IsZero()
}

func (r *Repository) validateSnapshotJob(
	proof *contract.LeaseProof,
	spec *JobSpec,
	job collection.JobContract,
) error {
	definition, _, err := spec.validate(r.contracts)
	if err != nil {
		return fmt.Errorf("snapshot invariant: %w", snapshotInvariant("target snapshot job spec is invalid"))
	}

	if contractErr := job.Validate(); contractErr != nil || !sameJobContract(definition, job) {
		return fmt.Errorf("snapshot invariant: %w", snapshotInvariant("target snapshot job contract does not match"))
	}

	subject, err := ExpectedLeaseSubject(job, spec.SubjectKey)
	if err != nil || subject != spec.SubjectKey {
		return fmt.Errorf("snapshot invariant: %w", snapshotInvariant("target snapshot lease subject does not match"))
	}

	key, err := BuildJobKey(job.ID(), subject)
	if err != nil || key != spec.JobKey {
		return fmt.Errorf("snapshot invariant: %w", snapshotInvariant("target snapshot job key does not match"))
	}

	if proof.JobKey != key {
		return fmt.Errorf("snapshot invariant: %w", snapshotInvariant("target snapshot lease proof does not match"))
	}

	return nil
}

func sameJobContract(left, right collection.JobContract) bool {
	return left.ID() == right.ID() && left.Class() == right.Class() && left.Membership() == right.Membership() &&
		left.LeaseSubject() == right.LeaseSubject() && slices.Equal(left.Emissions(), right.Emissions()) &&
		slices.Equal(left.CadenceKinds(), right.CadenceKinds()) && slices.Equal(left.RosterKinds(), right.RosterKinds())
}

func (q *snapshotQuery) loadExact(ctx context.Context, tx dbx.Tx, subject string) (collection.TargetSnapshot, error) {
	rows, err := tx.Query(ctx, loadExactSQL, q.current, q.kindValues, subject)
	if err != nil {
		return collection.TargetSnapshot{}, fmt.Errorf("load exact target snapshot: query targets: %w", err)
	}
	defer rows.Close()

	enabled, err := scanExactTargetRows(rows, q.maxRosterRows)
	if err != nil {
		return collection.TargetSnapshot{}, fmt.Errorf("scan exact target rows: %w", err)
	}

	if len(enabled) != len(q.requested) {
		return collection.TargetSnapshot{}, fmt.Errorf("snapshot invariant: %w", snapshotInvariant("exact target snapshot row count does not match requested kinds"))
	}

	out, err := collection.NewExactTargetSnapshot(q.acquired, subject, q.requested, enabled)
	if err != nil {
		return out, fmt.Errorf("exact target snapshot: %w", err)
	}

	return out, nil
}

func scanExactTargetRows(rows pgx.Rows, maxRosterRows int) (map[contract.ObservationKind]bool, error) {
	result := exactTargetRows{enabled: make(map[contract.ObservationKind]bool), maxRosterRows: maxRosterRows}

	for rows.Next() {
		kind, isEnabled, err := scanExactTargetRow(rows)
		if err != nil {
			return nil, fmt.Errorf("scan exact target row: %w", err)
		}

		if err := result.add(kind, isEnabled); err != nil {
			return nil, fmt.Errorf("add: %w", err)
		}
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("load exact target snapshot: read targets: %w", err)
	}

	return result.enabled, nil
}

type exactTargetRows struct {
	enabled       map[contract.ObservationKind]bool
	enabledCount  int
	maxRosterRows int
}

func (r *exactTargetRows) add(kind contract.ObservationKind, enabled bool) error {
	if _, exists := r.enabled[kind]; exists {
		return fmt.Errorf("snapshot invariant: %w", snapshotInvariant("exact target snapshot returned a duplicate kind"))
	}

	r.enabled[kind] = enabled
	if !enabled {
		return nil
	}

	r.enabledCount++
	if r.enabledCount > r.maxRosterRows {
		return collecterr.New(contract.ErrorTargetRosterTooLarge, contract.ClassResourceLimit, "target roster exceeds configured limit")
	}

	return nil
}

func scanExactTargetRow(rows pgx.Rows) (contract.ObservationKind, bool, error) {
	var (
		kind             contract.ObservationKind
		current, enabled bool
	)

	if err := rows.Scan(&kind, &current, &enabled); err != nil {
		return "", false, fmt.Errorf("load exact target snapshot: scan target: %w", err)
	}

	if !current {
		return "", false, collection.ErrProjectionStale
	}

	return kind, enabled, nil
}

func (q *snapshotQuery) loadProjection(ctx context.Context, tx dbx.Tx) (collection.TargetSnapshot, error) {
	rows, err := tx.Query(ctx, loadProjectionSQL, q.current, q.kindValues, q.maxRosterRows)
	if err != nil {
		return collection.TargetSnapshot{}, fmt.Errorf("load projection target snapshot: query targets: %w", err)
	}
	defer rows.Close()

	values, err := scanProjectionTargetRows(rows, q.requested)
	if err != nil {
		return collection.TargetSnapshot{}, fmt.Errorf("scan projection target rows: %w", err)
	}

	out, err := collection.NewProjectionTargetSnapshot(q.acquired, q.requested, values, q.maxRosterRows)
	if err != nil {
		return out, fmt.Errorf("projection target snapshot: %w", err)
	}

	return out, nil
}

func scanProjectionTargetRows(
	rows pgx.Rows,
	requested []contract.ObservationKind,
) (map[contract.ObservationKind][]string, error) {
	result := projectionTargetRows{values: make(map[contract.ObservationKind][]string, len(requested))}

	for rows.Next() {
		kind, subject, err := scanProjectionTargetRow(rows)
		if err != nil {
			return nil, fmt.Errorf("scan projection target row: %w", err)
		}

		result.add(kind, subject)
	}

	if err := rows.Err(); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("load projection target snapshot: read targets: %w", err)
	}

	return result.values, nil
}

type projectionTargetRows struct {
	values map[contract.ObservationKind][]string
}

func (r *projectionTargetRows) add(kind contract.ObservationKind, subject *string) {
	if _, exists := r.values[kind]; !exists {
		r.values[kind] = []string{}
	}

	if subject != nil {
		r.values[kind] = append(r.values[kind], *subject)
	}
}

func scanProjectionTargetRow(rows pgx.Rows) (contract.ObservationKind, *string, error) {
	var (
		kind    contract.ObservationKind
		current bool
		subject *string
	)

	if err := rows.Scan(&kind, &current, &subject); err != nil {
		return "", nil, fmt.Errorf("load projection target snapshot: scan target: %w", err)
	}

	if !current {
		return "", nil, collection.ErrProjectionStale
	}

	return kind, subject, nil
}

func snapshotInvariant(message string) error {
	return collecterr.New(contract.ErrorInternalInvariant, contract.ClassInternal, message)
}
