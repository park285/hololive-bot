package joblease

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/service/youtube/sourceobservation"
)

func (r *Repository) candidateDefinition(provider contract.Provider, jobKind string, limit int) (sourceobservation.JobContract, []contract.ObservationKind, error) {
	if r == nil || r.pool == nil || r.contracts == nil || !provider.Valid() || limit < 1 || limit > r.config.AcquisitionBatch {
		return sourceobservation.JobContract{}, nil, fmt.Errorf("list collection job candidates: %w", ErrInvalidJob)
	}
	definition, ok := r.contracts.Definition(jobKind)
	if !ok {
		return sourceobservation.JobContract{}, nil, fmt.Errorf("list collection job candidates: %w: unknown job kind", ErrInvalidJob)
	}
	kinds := emissionKinds(definition, provider)
	if len(kinds) == 0 {
		return sourceobservation.JobContract{}, nil, fmt.Errorf("list collection job candidates: %w: provider has no emissions", ErrInvalidJob)
	}
	return definition, kinds, nil
}

func (r *Repository) currentProjectionGeneration(ctx context.Context) (int64, error) {
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

func (r *Repository) globalCandidate(
	ctx context.Context,
	provider contract.Provider,
	jobKind string,
	generation int64,
	kindValues []string,
	definition sourceobservation.JobContract,
) ([]JobSpec, error) {
	subject := definition.FixedSubject
	if subject == "" {
		subject = "global:" + jobKind
	}
	interval, err := r.loadCandidateInterval(ctx, generation, kindValues, definition.Membership, subject)
	if err != nil {
		return nil, err
	}
	return []JobSpec{{
		JobKey: "collector:" + string(provider) + ":" + jobKind + ":global", Provider: provider,
		Class: definition.Class, CollectionJobKind: jobKind, SubjectKey: subject, PollInterval: interval,
	}}, nil
}

func (r *Repository) scanTargetCandidates(
	ctx context.Context,
	provider contract.Provider,
	jobKind string,
	generation int64,
	kindValues []string,
	class string,
	limit int,
) ([]JobSpec, error) {
	rows, err := r.pool.Query(
		ctx,
		mustSQL("repository_candidates_0144_02.sql"),
		generation,
		kindValues,
		limit,
		string(provider),
		jobKind,
	)
	if err != nil {
		return nil, fmt.Errorf("list collection job candidates: query targets: %w", err)
	}
	defer rows.Close()
	return collectTargetCandidates(rows, provider, jobKind, class, limit)
}

func collectTargetCandidates(
	rows interface {
		Next() bool
		Scan(dest ...any) error
		Err() error
	},
	provider contract.Provider,
	jobKind, class string,
	limit int,
) ([]JobSpec, error) {
	result := make([]JobSpec, 0, limit)
	for rows.Next() {
		spec, err := scanTargetCandidate(rows, provider, jobKind, class)
		if err != nil {
			return nil, err
		}
		result = append(result, spec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list collection job candidates: read targets: %w", err)
	}
	return result, nil
}

func scanTargetCandidate(
	rows interface {
		Scan(dest ...any) error
	},
	provider contract.Provider,
	jobKind, class string,
) (JobSpec, error) {
	var subject string
	var minIntervalMS int64
	var maxIntervalMS int64
	if err := rows.Scan(&subject, &minIntervalMS, &maxIntervalMS); err != nil {
		return JobSpec{}, fmt.Errorf("list collection job candidates: scan target: %w", err)
	}
	if minIntervalMS <= 0 {
		return JobSpec{}, fmt.Errorf("list collection job candidates: %w: subject bundle has no poll interval", ErrInvalidJob)
	}
	if minIntervalMS != maxIntervalMS {
		return JobSpec{}, fmt.Errorf("list collection job candidates: %w: subject bundle has mixed poll intervals", ErrInvalidJob)
	}
	return JobSpec{
		JobKey: "collector:" + string(provider) + ":" + jobKind + ":" + subject, Provider: provider, Class: class,
		CollectionJobKind: jobKind, SubjectKey: subject,
		PollInterval: time.Duration(minIntervalMS) * time.Millisecond,
	}, nil
}

func (r *Repository) EnabledSubjects(ctx context.Context, generation int64, kind contract.ObservationKind) ([]string, error) {
	if r == nil || r.pool == nil || generation <= 0 || !kind.Valid() {
		return nil, fmt.Errorf("list enabled collection subjects: %w", ErrInvalidJob)
	}
	rows, err := r.pool.Query(
		ctx,
		mustSQL("repository_enabled_subjects_0144_03.sql"),
		generation,
		string(kind),
	)
	if err != nil {
		return nil, fmt.Errorf("list enabled collection subjects: query targets: %w", err)
	}
	defer rows.Close()
	return scanEnabledSubjects(rows)
}

func scanEnabledSubjects(rows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}) ([]string, error) {
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
	err := r.pool.QueryRow(
		ctx,
		mustSQL("repository_target_bundle_0144_04.sql"),
		generation,
		kinds,
		exact,
		subject,
	).Scan(&count, &minIntervalMS, &maxIntervalMS)
	if err != nil {
		return 0, fmt.Errorf("list collection job candidates: inspect global target set: %w", err)
	}
	if count == 0 {
		return 0, ErrTargetDisabled
	}
	if minIntervalMS <= 0 {
		return 0, fmt.Errorf("list collection job candidates: %w: target bundle has no poll interval", ErrInvalidJob)
	}
	_ = membership
	if minIntervalMS != maxIntervalMS {
		return 0, fmt.Errorf("list collection job candidates: %w: target bundle has mixed poll intervals", ErrInvalidJob)
	}
	return time.Duration(minIntervalMS) * time.Millisecond, nil
}
