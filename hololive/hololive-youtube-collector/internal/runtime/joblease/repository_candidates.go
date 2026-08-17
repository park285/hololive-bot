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
	"github.com/kapu/hololive-shared/pkg/service/youtube/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
)

type CandidatePage struct {
	Jobs      []JobSpec
	Truncated bool
}

func (r *Repository) CurrentProjectionGeneration(ctx context.Context) (int64, error) {
	if r == nil || r.pool == nil {
		return 0, fmt.Errorf("list collection job candidates: %w", ErrInvalidJob)
	}
	var generation int64
	err := r.pool.QueryRow(ctx, mustSQL("repository_projection_current_0144_01.sql")).Scan(&generation)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrProjectionStale
	}
	if err != nil {
		return 0, fmt.Errorf("list collection job candidates: load current projection: %w", err)
	}
	return generation, nil
}

func (r *Repository) CandidatesForProjection(
	ctx context.Context,
	generation int64,
	job sourceobservation.JobContract,
	excludedJobKeys []string,
	limit int,
) (CandidatePage, error) {
	if err := r.validateCandidateRequest(generation, job, limit); err != nil {
		return CandidatePage{}, err
	}
	excluded, err := normalizeExcludedJobKeys(excludedJobKeys, r.config.QueueCapacity)
	if err != nil {
		return CandidatePage{}, err
	}
	kindValues, err := cadenceKindValues(job)
	if err != nil {
		return CandidatePage{}, err
	}
	if job.Class() == sourceobservation.JobClassGlobal {
		return r.globalCandidatesForProjection(ctx, generation, job, kindValues, excluded)
	}
	return r.subjectCandidatesForProjection(ctx, generation, job, kindValues, excluded, limit)
}

func (r *Repository) validateCandidateRequest(generation int64, job sourceobservation.JobContract, limit int) error {
	if r == nil || r.pool == nil || r.contracts == nil {
		return fmt.Errorf("list collection job candidates: %w", ErrInvalidJob)
	}
	if generation <= 0 || limit < 1 || limit > r.config.AcquisitionBatch {
		return fmt.Errorf("list collection job candidates: %w: generation or limit is outside bounds", ErrInvalidJob)
	}
	if err := job.Validate(); err != nil {
		return collecterr.Wrap(collecterr.Internal, collecterr.ClassInternal, err)
	}
	if !r.isCanonicalJob(job) {
		return collecterr.New(collecterr.Internal, collecterr.ClassInternal, "list collection job candidates: job contract is not canonical")
	}
	return nil
}

func (r *Repository) isCanonicalJob(job sourceobservation.JobContract) bool {
	canonical, ok := r.contracts.Definition(job.ID())
	return ok && canonical.Class() == job.Class() && canonical.Membership() == job.Membership() &&
		canonical.LeaseSubject() == job.LeaseSubject()
}

func (r *Repository) subjectCandidatesForProjection(
	ctx context.Context,
	generation int64,
	job sourceobservation.JobContract,
	kindValues []string,
	excluded []string,
	limit int,
) (CandidatePage, error) {
	rows, err := r.pool.Query(
		ctx,
		mustSQL("repository_candidates_0144_02.sql"),
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
	return collectCandidatePage(rows, job, limit)
}

func (r *Repository) globalCandidatesForProjection(
	ctx context.Context,
	generation int64,
	job sourceobservation.JobContract,
	kindValues []string,
	excluded []string,
) (CandidatePage, error) {
	subject, err := ExpectedLeaseSubject(job, "")
	if err != nil {
		return CandidatePage{}, collecterr.Wrap(collecterr.Internal, collecterr.ClassInternal, err)
	}
	jobKey, err := BuildJobKey(job.ID(), subject)
	if err != nil {
		return CandidatePage{}, collecterr.Wrap(collecterr.Internal, collecterr.ClassInternal, err)
	}
	rows, err := r.pool.Query(
		ctx,
		mustSQL("repository_candidates_global_0144_17.sql"),
		generation,
		kindValues,
		job.Membership() == sourceobservation.JobMembershipExactSubject,
		subject,
		jobKey,
		excluded,
	)
	if err != nil {
		return CandidatePage{}, fmt.Errorf("list collection job candidates: query global target set: %w", err)
	}
	defer rows.Close()
	return collectCandidatePage(rows, job, 1)
}

func collectCandidatePage(
	rows interface {
		Next() bool
		Scan(dest ...any) error
		Err() error
	},
	job sourceobservation.JobContract,
	limit int,
) (CandidatePage, error) {
	jobs := make([]JobSpec, 0, limit)
	sawRow := false
	projectionCurrent := false
	for rows.Next() {
		row, err := scanCandidateRow(rows)
		if err != nil {
			return CandidatePage{}, err
		}
		sawRow = true
		projectionCurrent = row.current
		if !row.current || row.subject == "" {
			continue
		}
		spec, err := specFromCandidateRow(job, row)
		if err != nil {
			return CandidatePage{}, err
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
		return CandidatePage{}, ErrProjectionStale
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
	var current bool
	var subject sql.NullString
	var minMS sql.NullInt64
	var maxMS sql.NullInt64
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

func specFromCandidateRow(job sourceobservation.JobContract, row candidateRow) (JobSpec, error) {
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

func cadenceKindValues(job sourceobservation.JobContract) ([]string, error) {
	kinds := job.CadenceKinds()
	if len(kinds) == 0 {
		return nil, collecterr.New(collecterr.Internal, collecterr.ClassInternal, "list collection job candidates: cadence kinds are empty")
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
	unique := make([]string, 0, len(cloned))
	for _, key := range cloned {
		if invalidExcludedJobKey(key) {
			return nil, fmt.Errorf("list collection job candidates: %w: excluded job key is outside bounds", ErrInvalidJob)
		}
		if duplicateLast(unique, key) {
			continue
		}
		unique = append(unique, key)
	}
	if len(unique) > capacity {
		return nil, fmt.Errorf("list collection job candidates: %w: excluded job keys exceed queue capacity", ErrInvalidJob)
	}
	return unique, nil
}

func invalidExcludedJobKey(key string) bool {
	return strings.TrimSpace(key) != key || key == ""
}

func duplicateLast(keys []string, key string) bool {
	return len(keys) > 0 && keys[len(keys)-1] == key
}
