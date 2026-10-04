package joblease

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collection"
)

// ErrCandidateContract는 특정 런너 또는 target bundle의 후보 계약 오류입니다.
// DB·projection·요청 예산 오류는 이 표식을 붙이지 않고 cycle 전체를 중단합니다.
var ErrCandidateContract = errors.New("candidate contract is invalid")

func candidateContractError(err error) error {
	return fmt.Errorf("%w: %w", ErrCandidateContract, err)
}

type CandidatePage struct {
	Jobs      []JobSpec
	Truncated bool
}

func (r *Repository) CurrentProjectionGeneration(ctx context.Context) (int64, error) {
	if r == nil || r.pool == nil {
		return 0, fmt.Errorf("list collection job candidates: %w", ErrInvalidJob)
	}

	var generation int64

	err := r.pool.QueryRow(ctx, sqlProjectionCurrent).Scan(&generation)

	if errors.Is(err, pgx.ErrNoRows) {
		return 0, collection.ErrProjectionStale
	}

	if err != nil {
		return 0, fmt.Errorf("list collection job candidates: load current projection: %w", err)
	}

	return generation, nil
}

func (r *Repository) CandidatesForProjection(
	ctx context.Context,
	generation int64,
	job collection.JobContract,
	excludedJobKeys []string,
	limit int,
) (CandidatePage, error) {
	if err := r.validateCandidateRequest(generation, job, limit); err != nil {
		return CandidatePage{}, fmt.Errorf("validate candidate request: %w", err)
	}

	excluded, err := normalizeExcludedJobKeys(excludedJobKeys, r.config.QueueCapacity)
	if err != nil {
		return CandidatePage{}, fmt.Errorf("normalize excluded job keys: %w", err)
	}

	kindValues, err := cadenceKindValues(job)
	if err != nil {
		return CandidatePage{}, fmt.Errorf("cadence kind values: %w", err)
	}

	if job.Class() == collection.JobClassGlobal {
		out, globalErr := r.globalCandidatesForProjection(ctx, generation, job, kindValues, excluded)
		if globalErr != nil {
			return out, fmt.Errorf("global candidates for projection: %w", globalErr)
		}

		return out, nil
	}

	out, err := r.subjectCandidatesForProjection(ctx, generation, job, kindValues, excluded, limit)
	if err != nil {
		return out, fmt.Errorf("subject candidates for projection: %w", err)
	}

	return out, nil
}

func (r *Repository) validateCandidateRequest(generation int64, job collection.JobContract, limit int) error {
	if r == nil || r.pool == nil || r.contracts == nil {
		return fmt.Errorf("list collection job candidates: %w", ErrInvalidJob)
	}

	if generation <= 0 || limit < 1 || limit > r.config.AcquisitionBatch {
		return fmt.Errorf("list collection job candidates: %w: generation or limit is outside bounds", ErrInvalidJob)
	}

	if err := job.Validate(); err != nil {
		return candidateContractError(collecterr.Wrap(collecterr.Internal, collecterr.ClassInternal, err))
	}

	if !r.isCanonicalJob(job) {
		return candidateContractError(collecterr.New(collecterr.Internal, collecterr.ClassInternal, "list collection job candidates: job contract is not canonical"))
	}

	return nil
}

func (r *Repository) isCanonicalJob(job collection.JobContract) bool {
	canonical, ok := r.contracts.Definition(job.ID())
	return ok && canonical.Class() == job.Class() && canonical.Membership() == job.Membership() && canonical.LeaseSubject() == job.LeaseSubject()
}

func (r *Repository) subjectCandidatesForProjection(
	ctx context.Context,
	generation int64,
	job collection.JobContract,
	kindValues []string,
	excluded []string,
	limit int,
) (CandidatePage, error) {
	rows, err := r.pool.Query(
		ctx,
		sqlCandidates,
		generation,
		kindValues,
		string(job.ID().Provider),
		string(job.ID().Kind),
		excluded,
		limit,
	)
	if err != nil {
		return CandidatePage{}, fmt.Errorf("list collection job candidates: query targets: %w", err)
	}
	defer rows.Close()

	out, err := collectCandidatePage(rows, job, limit)
	if err != nil {
		return out, fmt.Errorf("collect candidate page: %w", err)
	}

	return out, nil
}

func (r *Repository) globalCandidatesForProjection(
	ctx context.Context,
	generation int64,
	job collection.JobContract,
	kindValues []string,
	excluded []string,
) (CandidatePage, error) {
	subject, err := ExpectedLeaseSubject(job, "")
	if err != nil {
		return CandidatePage{}, candidateContractError(collecterr.Wrap(collecterr.Internal, collecterr.ClassInternal, err))
	}

	jobKey, err := BuildJobKey(job.ID(), subject)
	if err != nil {
		return CandidatePage{}, candidateContractError(collecterr.Wrap(collecterr.Internal, collecterr.ClassInternal, err))
	}

	rows, err := r.pool.Query(
		ctx,
		sqlCandidatesGlobal,
		generation,
		kindValues,
		job.Membership() == collection.JobMembershipExactSubject,
		subject,
		jobKey,
		excluded,
	)
	if err != nil {
		return CandidatePage{}, fmt.Errorf("list collection job candidates: query global target set: %w", err)
	}
	defer rows.Close()

	out, err := collectCandidatePage(rows, job, 1)
	if err != nil {
		return out, fmt.Errorf("collect candidate page: %w", err)
	}

	return out, nil
}

func collectCandidatePage(
	rows interface {
		Next() bool
		Scan(dest ...any) error
		Err() error
	},
	job collection.JobContract,
	limit int,
) (CandidatePage, error) {
	jobs := make([]JobSpec, 0, limit)
	sawRow := false
	projectionCurrent := false

	var contractErr error

	for rows.Next() {
		row, err := scanCandidateRow(rows)
		if err != nil {
			return CandidatePage{}, fmt.Errorf("scan candidate row: %w", err)
		}

		sawRow = true
		projectionCurrent = row.current

		if !row.current || row.subject == "" {
			continue
		}

		spec, err := specFromCandidateRow(job, row)
		if err != nil {
			contractErr = errors.Join(contractErr, fmt.Errorf("spec from candidate row: %w", candidateContractError(err)))
			continue
		}

		jobs = append(jobs, spec)
	}

	if err := rows.Err(); err != nil {
		return CandidatePage{}, fmt.Errorf("list collection job candidates: read targets: %w", err)
	}

	if !sawRow {
		return CandidatePage{}, collecterr.New(collecterr.Internal, collecterr.ClassInternal, "list collection job candidates: candidate page is missing projection status")
	}

	if !projectionCurrent {
		return CandidatePage{}, collection.ErrProjectionStale
	}

	if contractErr != nil {
		return CandidatePage{}, contractErr
	}

	truncated := len(jobs) > limit
	if truncated {
		jobs = jobs[:limit]
	}

	return CandidatePage{Jobs: jobs, Truncated: truncated}, nil
}

type candidateRow struct {
	current bool
	subject string
	minMS   int64
	maxMS   int64
}

func scanCandidateRow(rows interface{ Scan(dest ...any) error }) (candidateRow, error) {
	var (
		current bool
		subject sql.NullString
		minMS   sql.NullInt64
		maxMS   sql.NullInt64
	)

	if err := rows.Scan(&current, &subject, &minMS, &maxMS); err != nil {
		return candidateRow{}, fmt.Errorf("list collection job candidates: scan target: %w", err)
	}

	row := candidateRow{current: current}

	if subject.Valid {
		row.subject = subject.String
	}

	if minMS.Valid {
		row.minMS = minMS.Int64
	}

	if maxMS.Valid {
		row.maxMS = maxMS.Int64
	}

	return row, nil
}

func specFromCandidateRow(job collection.JobContract, row candidateRow) (JobSpec, error) {
	if row.minMS <= 0 {
		return JobSpec{}, collecterr.New(collecterr.Internal, collecterr.ClassInternal, "list collection job candidates: target bundle has no poll interval")
	}

	if row.minMS != row.maxMS {
		return JobSpec{}, collecterr.New(collecterr.Internal, collecterr.ClassInternal, "list collection job candidates: target bundle has mixed poll intervals")
	}

	jobKey, err := BuildJobKey(job.ID(), row.subject)
	if err != nil {
		return JobSpec{}, collecterr.Wrap(collecterr.Internal, collecterr.ClassInternal, err)
	}

	return JobSpec{
		JobKey:            jobKey,
		Provider:          job.ID().Provider,
		Class:             string(job.Class()),
		CollectionJobKind: string(job.ID().Kind),
		SubjectKey:        row.subject,
		PollInterval:      time.Duration(row.minMS) * time.Millisecond,
	}, nil
}

func cadenceKindValues(job collection.JobContract) ([]string, error) {
	kinds := job.CadenceKinds()
	if len(kinds) == 0 {
		return nil, candidateContractError(collecterr.New(collecterr.Internal, collecterr.ClassInternal, "list collection job candidates: cadence kinds are empty"))
	}

	values := make([]string, len(kinds))
	for i, kind := range kinds {
		values[i] = string(kind)
	}

	return values, nil
}

func normalizeExcludedJobKeys(keys []string, capacity int) ([]string, error) {
	if capacity < 1 {
		return nil, fmt.Errorf("list collection job candidates: %w: queue capacity is outside bounds", ErrInvalidJob)
	}

	if len(keys) == 0 {
		return []string{}, nil
	}

	cloned := slices.Clone(keys)
	slices.Sort(cloned)

	unique := slices.Compact(cloned)
	for _, key := range unique {
		if strings.TrimSpace(key) != key || key == "" {
			return nil, fmt.Errorf("list collection job candidates: %w: excluded job key is outside bounds", ErrInvalidJob)
		}
	}

	if len(unique) > capacity {
		return nil, fmt.Errorf("list collection job candidates: %w: excluded job keys exceed queue capacity", ErrInvalidJob)
	}

	return unique, nil
}
