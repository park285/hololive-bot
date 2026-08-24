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
		return fmt.Errorf("load publish fence: %w", err)
	}

	if projectionErr := v.verifyProjection(ctx, tx, proof.ProjectionGeneration); projectionErr != nil {
		return fmt.Errorf("verify projection: %w", projectionErr)
	}

	subjects, kinds, err := v.collectPublishSubjects(&job, observations)
	if err != nil {
		return fmt.Errorf("collect publish subjects: %w", err)
	}

	if err := v.verifyTargetsEnabled(ctx, tx, proof.ProjectionGeneration, subjects, kinds); err != nil {
		return fmt.Errorf("verify targets enabled: %w", err)
	}

	return nil
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
	var (
		job      publishFenceJob
		jobClass string
	)

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

	definition, ok := v.jobs.Definition(JobID{Provider: contract.Provider(job.provider), Kind: JobKind(job.collectionJobKind)})
	if !ok || string(definition.Class()) != jobClass || leaseSubjectMismatch(definition, job.jobSubject) {
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
			return nil, nil, fmt.Errorf("validate publish observation: %w", err)
		}

		subjects[i] = observations[i].SubjectKey
		kinds[i] = string(observations[i].ObservationKind)
	}

	return subjects, kinds, nil
}

func (v sqlPublishFenceVerifier) validatePublishObservation(job *publishFenceJob, observation *contract.Envelope, index int) error {
	if job.provider != string(observation.Provider) ||
		!v.jobs.Allows(JobID{Provider: observation.Provider, Kind: JobKind(job.collectionJobKind)}, observation.ObservationKind) {
		return fmt.Errorf("verify collection job emission %d: %w", index, ErrTargetDisabled)
	}

	if job.definition.Membership() == JobMembershipExactSubject && observation.SubjectKey != job.jobSubject {
		return fmt.Errorf("verify collection job membership %d: %w", index, ErrTargetDisabled)
	}

	if job.definition.Membership() != JobMembershipExactSubject && job.definition.Membership() != JobMembershipCurrentProjection {
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

type preparedPublishBatch struct {
	input        PublishBatchInput
	observations []byte
	contracts    []byte
}

type leaseTerminalFunc func(
	context.Context,
	dbx.Tx,
	*contract.LeaseProof,
	PublishBatchResult,
	bool,
) error

func (r *Repository) PublishBatch(
	ctx context.Context,
	input *PublishBatchInput,
) (PublishBatchResult, error) {
	if err := r.validate(); err != nil {
		return PublishBatchResult{}, fmt.Errorf("validate: %w", err)
	}

	prepared, err := preparePublishBatch(input)
	if err != nil {
		return PublishBatchResult{}, fmt.Errorf("prepare publish batch: %w", err)
	}

	out, err := r.runPreparedPublish(ctx, &prepared, r.completePublishTerminal)
	if err != nil {
		return out, fmt.Errorf("run prepared publish: %w", err)
	}

	return out, nil
}

func (r *Repository) PublishBatchAndDefer(
	ctx context.Context,
	input *PublishBatchInput,
	deferInput DeferCollectionInput,
) (PublishBatchResult, error) {
	if err := r.validate(); err != nil {
		return PublishBatchResult{}, fmt.Errorf("validate: %w", err)
	}

	if err := deferInput.Validate(); err != nil {
		return PublishBatchResult{}, fmt.Errorf("publish source observation batch: %w", err)
	}

	prepared, err := preparePublishBatch(input)
	if err != nil {
		return PublishBatchResult{}, fmt.Errorf("prepare publish batch: %w", err)
	}

	out, err := r.runPreparedPublish(ctx, &prepared, deferPublishTerminal(deferInput))
	if err != nil {
		return out, fmt.Errorf("run prepared publish: %w", err)
	}

	return out, nil
}

func (r *Repository) runPreparedPublish(
	ctx context.Context,
	prepared *preparedPublishBatch,
	terminal leaseTerminalFunc,
) (PublishBatchResult, error) {
	out, err := dbx.InPgxTxWithResult(ctx, r.pool, func(tx dbx.Tx) (PublishBatchResult, error) {
		return r.publishPreparedTx(ctx, tx, prepared, terminal)
	})
	if err != nil {
		return out, fmt.Errorf("in pgx tx with result: %w", err)
	}

	return out, nil
}

func (r *Repository) publishPreparedTx(
	ctx context.Context,
	tx dbx.Tx,
	prepared *preparedPublishBatch,
	terminal leaseTerminalFunc,
) (PublishBatchResult, error) {
	if err := r.verifyPreparedPublish(ctx, tx, prepared); err != nil {
		return PublishBatchResult{}, fmt.Errorf("verify prepared publish: %w", err)
	}

	result, collision, err := r.publishPreparedObservations(ctx, tx, prepared)
	if err != nil {
		return PublishBatchResult{}, fmt.Errorf("publish prepared observations: %w", err)
	}

	if err := terminal(ctx, tx, &prepared.input.Lease, result, collision); err != nil {
		return PublishBatchResult{}, fmt.Errorf("terminal: %w", err)
	}

	if err := r.applyPublishFault(ctx, tx, faultBeforeCommit); err != nil {
		return PublishBatchResult{}, fmt.Errorf("apply publish fault: %w", err)
	}

	return result, nil
}

func (r *Repository) verifyPreparedPublish(
	ctx context.Context,
	tx dbx.Tx,
	prepared *preparedPublishBatch,
) error {
	if err := r.fenceVerifier.Verify(
		ctx,
		tx,
		&prepared.input.Lease,
		prepared.input.Observations,
	); err != nil {
		return fmt.Errorf("publish source observation batch: verify fence: %w", err)
	}

	if err := r.applyPublishFault(ctx, tx, faultAfterFenceVerify); err != nil {
		return fmt.Errorf("apply publish fault: %w", err)
	}

	if err := verifyCurrentContracts(ctx, tx, prepared.contracts); err != nil {
		return fmt.Errorf("verify current contracts: %w", err)
	}

	if err := r.applyPublishFault(ctx, tx, faultAfterContractCheck); err != nil {
		return fmt.Errorf("apply publish fault: %w", err)
	}

	return nil
}

func (r *Repository) publishPreparedObservations(
	ctx context.Context,
	tx dbx.Tx,
	prepared *preparedPublishBatch,
) (PublishBatchResult, bool, error) {
	result, collision, err := publishObservationSet(
		ctx,
		tx,
		prepared.observations,
		len(prepared.input.Observations),
	)
	if err != nil {
		return PublishBatchResult{}, false, fmt.Errorf("publish observation set: %w", err)
	}

	if r.rewritePublishResult != nil {
		result = r.rewritePublishResult(result)
	}

	if err := r.applyPublishFault(ctx, tx, faultAfterObservationSet); err != nil {
		return PublishBatchResult{}, false, fmt.Errorf("apply publish fault: %w", err)
	}

	if err := ValidatePublishBatchResult(len(prepared.input.Observations), result); err != nil {
		return PublishBatchResult{}, false, fmt.Errorf("validate publish batch result: %w", err)
	}

	if err := r.applyPublishFault(ctx, tx, faultBeforeTerminal); err != nil {
		return PublishBatchResult{}, false, fmt.Errorf("apply publish fault: %w", err)
	}

	return result, collision, nil
}

func (r *Repository) applyPublishFault(ctx context.Context, tx dbx.Tx, point publishFaultPoint) error {
	if r == nil || r.publishFault == nil {
		return nil
	}

	if err := r.publishFault(ctx, tx, point); err != nil {
		return fmt.Errorf("publish fault: %w", err)
	}

	return nil
}
