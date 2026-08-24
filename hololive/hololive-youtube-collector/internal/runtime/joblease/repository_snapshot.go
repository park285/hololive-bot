package joblease

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/jackc/pgx/v5"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/service/youtube/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
)

func (r *Repository) LoadTargetSnapshot(
	ctx context.Context,
	proof *contract.LeaseProof,
	spec *JobSpec,
	job sourceobservation.JobContract,
	maxRosterRows int,
) (TargetSnapshot, error) {
	if err := r.validateSnapshotRequest(proof, spec, job, maxRosterRows); err != nil {
		return TargetSnapshot{}, fmt.Errorf("validate snapshot request: %w", err)
	}

	requested := job.RequestedKinds()
	kindValues := make([]string, len(requested))

	for index, kind := range requested {
		kindValues[index] = string(kind)
	}

	switch job.Membership() {
	case sourceobservation.JobMembershipExactSubject:
		out, err := r.loadExactSnapshotResult(ctx, proof.ProjectionGeneration, spec.SubjectKey, requested, kindValues, maxRosterRows)

		return out, errors.Join(err)
	case sourceobservation.JobMembershipCurrentProjection:
		out, err := r.loadProjectionSnapshotResult(ctx, proof.ProjectionGeneration, requested, kindValues, maxRosterRows)

		return out, errors.Join(err)
	default:
		return TargetSnapshot{}, fmt.Errorf("snapshot invariant: %w", snapshotInvariant("target snapshot membership is invalid"))
	}
}

func (r *Repository) loadExactSnapshotResult(ctx context.Context, generation int64, subject string, requested []contract.ObservationKind, kindValues []string, maxRosterRows int) (TargetSnapshot, error) {
	out, err := r.loadExactTargetSnapshot(ctx, generation, subject, requested, kindValues, maxRosterRows)
	if err != nil {
		return out, fmt.Errorf("load exact target snapshot: %w", err)
	}

	return out, nil
}

func (r *Repository) loadProjectionSnapshotResult(ctx context.Context, generation int64, requested []contract.ObservationKind, kindValues []string, maxRosterRows int) (TargetSnapshot, error) {
	out, err := r.loadProjectionTargetSnapshot(ctx, generation, requested, kindValues, maxRosterRows)
	if err != nil {
		return out, fmt.Errorf("load projection target snapshot: %w", err)
	}

	return out, nil
}

func (r *Repository) validateSnapshotRequest(
	proof *contract.LeaseProof,
	spec *JobSpec,
	job sourceobservation.JobContract,
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
	job sourceobservation.JobContract,
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

func sameJobContract(left, right sourceobservation.JobContract) bool {
	return left.ID() == right.ID() && left.Class() == right.Class() && left.Membership() == right.Membership() &&
		left.LeaseSubject() == right.LeaseSubject() && slices.Equal(left.Emissions(), right.Emissions()) &&
		slices.Equal(left.CadenceKinds(), right.CadenceKinds()) && slices.Equal(left.RosterKinds(), right.RosterKinds())
}

func (r *Repository) loadExactTargetSnapshot(
	ctx context.Context,
	generation int64,
	subject string,
	requested []contract.ObservationKind,
	kindValues []string,
	maxRosterRows int,
) (TargetSnapshot, error) {
	rows, err := r.pool.Query(ctx, mustSQL("repository_target_snapshot_exact_0144_15.sql"), generation, kindValues, subject)
	if err != nil {
		return TargetSnapshot{}, fmt.Errorf("load exact target snapshot: query targets: %w", err)
	}
	defer rows.Close()

	enabled, err := scanExactTargetRows(rows, maxRosterRows)
	if err != nil {
		return TargetSnapshot{}, fmt.Errorf("scan exact target rows: %w", err)
	}

	if len(enabled) != len(requested) {
		return TargetSnapshot{}, fmt.Errorf("snapshot invariant: %w", snapshotInvariant("exact target snapshot row count does not match requested kinds"))
	}

	out, err := newExactTargetSnapshot(generation, subject, requested, enabled)
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
		//nolint:wrapcheck // 오류 생성자가 만든 값이라 감쌀 하위 오류가 없다.
		return collecterr.New(collecterr.TargetRosterTooLarge, collecterr.ClassResourceLimit, "target roster exceeds configured limit")
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
		return "", false, ErrProjectionStale
	}

	return kind, enabled, nil
}

func (r *Repository) loadProjectionTargetSnapshot(
	ctx context.Context,
	generation int64,
	requested []contract.ObservationKind,
	kindValues []string,
	maxRosterRows int,
) (TargetSnapshot, error) {
	rows, err := r.pool.Query(ctx, mustSQL("repository_target_snapshot_projection_0144_16.sql"), generation, kindValues, maxRosterRows)
	if err != nil {
		return TargetSnapshot{}, fmt.Errorf("load projection target snapshot: query targets: %w", err)
	}
	defer rows.Close()

	values, err := scanProjectionTargetRows(rows, requested)
	if err != nil {
		return TargetSnapshot{}, fmt.Errorf("scan projection target rows: %w", err)
	}

	out, err := newProjectionTargetSnapshot(generation, requested, values, maxRosterRows)
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
		return "", nil, ErrProjectionStale
	}

	return kind, subject, nil
}
