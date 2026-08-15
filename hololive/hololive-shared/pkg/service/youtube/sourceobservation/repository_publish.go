package sourceobservation

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/dbx"
)

type sqlPublishFenceVerifier struct {
	jobs JobContractSet
}

func (v sqlPublishFenceVerifier) Verify(
	ctx context.Context,
	tx dbx.Tx,
	proof *contract.LeaseProof,
	observations []contract.Envelope,
) error {
	job, err := v.loadPublishFence(ctx, tx, proof)
	if err != nil {
		return err
	}
	if err := v.verifyProjection(ctx, tx, proof.ProjectionGeneration); err != nil {
		return err
	}
	subjects, kinds, err := v.collectPublishSubjects(&job, observations)
	if err != nil {
		return err
	}
	return v.verifyTargetsEnabled(ctx, tx, proof.ProjectionGeneration, subjects, kinds)
}

type publishFenceJob struct {
	provider          string
	collectionJobKind string
	definition        JobContract
	jobSubject        string
}

func (v sqlPublishFenceVerifier) loadPublishFence(
	ctx context.Context,
	tx dbx.Tx,
	proof *contract.LeaseProof,
) (publishFenceJob, error) {
	var job publishFenceJob
	var jobClass string
	err := tx.QueryRow(
		ctx,
		mustSQL("repository_publish_fence_0001_01.sql"),
		proof.JobKey,
		proof.OwnerInstance,
		proof.FenceEpoch,
		proof.ProjectionGeneration,
		proof.ScheduledFor,
	).Scan(&job.provider, &job.collectionJobKind, &jobClass, &job.jobSubject)
	if errors.Is(err, pgx.ErrNoRows) {
		return publishFenceJob{}, ErrCollectionFenceLost
	}
	if err != nil {
		return publishFenceJob{}, fmt.Errorf("verify collection job fence: %w", err)
	}
	if job.collectionJobKind != proof.CollectionJobKind {
		return publishFenceJob{}, ErrCollectionFenceLost
	}
	definition, ok := v.jobs.Definition(job.collectionJobKind)
	if !ok || definition.Class != jobClass || definition.FixedSubject != "" && definition.FixedSubject != job.jobSubject {
		return publishFenceJob{}, ErrCollectionFenceLost
	}
	job.definition = definition
	return job, nil
}

func (v sqlPublishFenceVerifier) verifyProjection(ctx context.Context, tx dbx.Tx, generation int64) error {
	var current int64
	err := tx.QueryRow(ctx, mustSQL("repository_projection_current_0002_02.sql"), generation).Scan(&current)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrProjectionStale
	}
	if err != nil {
		return fmt.Errorf("verify current collection projection: %w", err)
	}
	return nil
}

func (v sqlPublishFenceVerifier) collectPublishSubjects(
	job *publishFenceJob,
	observations []contract.Envelope,
) (subjectKeys, kindNames []string, err error) {
	subjects := make([]string, len(observations))
	kinds := make([]string, len(observations))
	for i := range observations {
		if err := v.validatePublishObservation(job, &observations[i], i); err != nil {
			return nil, nil, err
		}
		subjects[i] = observations[i].SubjectKey
		kinds[i] = string(observations[i].ObservationKind)
	}
	return subjects, kinds, nil
}

func (v sqlPublishFenceVerifier) validatePublishObservation(job *publishFenceJob, observation *contract.Envelope, index int) error {
	if job.provider != string(observation.Provider) ||
		!v.jobs.Allows(job.collectionJobKind, observation.Provider, observation.ObservationKind) {
		return fmt.Errorf("verify collection job emission %d: %w", index, ErrTargetDisabled)
	}
	if job.definition.Membership == JobMembershipExactSubject && observation.SubjectKey != job.jobSubject {
		return fmt.Errorf("verify collection job membership %d: %w", index, ErrTargetDisabled)
	}
	if job.definition.Membership != JobMembershipExactSubject && job.definition.Membership != JobMembershipCurrentProjection {
		return fmt.Errorf("verify collection job membership %d: %w", index, ErrTargetDisabled)
	}
	return nil
}

func (v sqlPublishFenceVerifier) verifyTargetsEnabled(
	ctx context.Context,
	tx dbx.Tx,
	generation int64,
	subjects, kinds []string,
) error {
	var allEnabled bool
	err := tx.QueryRow(ctx, mustSQL("repository_target_enabled_0003_03.sql"), generation, subjects, kinds).Scan(&allEnabled)
	if err != nil {
		return fmt.Errorf("verify collection targets: %w", err)
	}
	if !allEnabled {
		return fmt.Errorf("verify collection targets: %w", ErrTargetDisabled)
	}
	return nil
}

func (r *Repository) PublishBatch(
	ctx context.Context,
	input *PublishBatchInput,
) (PublishBatchResult, error) {
	if input == nil {
		return PublishBatchResult{}, fmt.Errorf("publish source observation batch: input is nil")
	}
	if err := r.validate(); err != nil {
		return PublishBatchResult{}, err
	}
	if err := validatePublishBatch(input); err != nil {
		return PublishBatchResult{}, fmt.Errorf("publish source observation batch: %w", err)
	}
	encoded, contracts, err := encodePublishBatch(input)
	if err != nil {
		return PublishBatchResult{}, err
	}
	return dbx.InPgxTxWithResult(ctx, r.pool, func(tx dbx.Tx) (PublishBatchResult, error) {
		return r.publishBatchTx(ctx, tx, input, encoded, contracts)
	})
}

func (r *Repository) publishBatchTx(
	ctx context.Context,
	tx dbx.Tx,
	input *PublishBatchInput,
	encoded []byte,
	contracts []byte,
) (PublishBatchResult, error) {
	if err := r.fenceVerifier.Verify(ctx, tx, &input.Lease, input.Observations); err != nil {
		return PublishBatchResult{}, fmt.Errorf("publish source observation batch: verify job fence: %w", err)
	}
	if err := verifyCurrentContracts(ctx, tx, contracts); err != nil {
		return PublishBatchResult{}, err
	}
	result, collision, err := publishObservationSet(ctx, tx, encoded, len(input.Observations))
	if err != nil {
		return PublishBatchResult{}, err
	}
	errorCode := ""
	if collision {
		errorCode = "observation_collision"
	}
	if err := completeCollectionJob(ctx, tx, &input.Lease, errorCode); err != nil {
		return PublishBatchResult{}, err
	}
	return result, nil
}
