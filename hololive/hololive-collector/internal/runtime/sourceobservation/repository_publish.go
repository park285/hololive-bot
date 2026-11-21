package sourceobservation

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/kapu/hololive-collector/internal/runtime/collection"
	"github.com/kapu/hololive-collector/internal/runtime/joblease"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/dbx"
)

type sqlPublishFenceVerifier struct {
	jobs collection.StaticJobContracts
}

type publishVerificationBatchSender interface {
	SendBatch(ctx context.Context, batch *pgx.Batch) pgx.BatchResults
}

func (v sqlPublishFenceVerifier) Verify(
	ctx context.Context,
	tx dbx.Tx,
	proof *contract.LeaseProof,
	observations []contract.Envelope,
	contracts []byte,
) (err error) {
	sender, ok := tx.(publishVerificationBatchSender)
	if !ok {
		return errors.New("verify publish fence: transaction does not support pipelined batches")
	}

	scope, err := v.publishMembershipScope(proof, observations)
	if err != nil {
		return fmt.Errorf("publish membership scope: %w", err)
	}

	results := sender.SendBatch(ctx, publishVerificationBatch(proof, scope, observations, contracts))
	if results == nil {
		return errors.New("verify publish fence: batch results are nil")
	}

	defer func() {
		if closeErr := results.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("close publish verification batch: %w", closeErr))
		}
	}()

	return v.verifyPublishResults(results, proof, observations)
}

func publishVerificationBatch(
	proof *contract.LeaseProof,
	scope collection.MembershipScope,
	observations []contract.Envelope,
	contracts []byte,
) *pgx.Batch {
	subjects, kinds := publishTargetKeys(observations)
	membershipQuery, membershipArgs := joblease.LeaseMembershipQuery(proof, scope)
	batch := &pgx.Batch{}

	batch.Queue(publishVerificationBatchSQL)
	batch.Queue(
		publishVerificationBatch2SQL,
		proof.JobKey,
		proof.OwnerInstance,
		proof.FenceEpoch,
		proof.ProjectionGeneration,
		proof.ScheduledFor,
	)
	batch.Queue(membershipQuery, membershipArgs...)
	batch.Queue(publishVerificationBatch3SQL, proof.ProjectionGeneration, subjects, kinds)
	batch.Queue(publishVerificationBatch4SQL, string(contracts))

	return batch
}

func (v sqlPublishFenceVerifier) verifyPublishResults(
	results pgx.BatchResults,
	proof *contract.LeaseProof,
	observations []contract.Envelope,
) error {
	projectionErr := verifyProjection(results)

	job, err := v.loadPublishFence(results, proof)
	if err != nil {
		return fmt.Errorf("load publish fence: %w", err)
	}

	if projectionErr != nil {
		return fmt.Errorf("verify projection: %w", projectionErr)
	}

	if err := joblease.ScanLeaseMembership(results.QueryRow()); err != nil {
		return fmt.Errorf("verify lease membership: %w", err)
	}

	if err := v.validatePublishObservations(&job, observations); err != nil {
		return fmt.Errorf("validate publish observations: %w", err)
	}

	if err := verifyTargetsEnabled(results); err != nil {
		return fmt.Errorf("verify targets enabled: %w", err)
	}

	if err := verifyCurrentContracts(results.QueryRow()); err != nil {
		return fmt.Errorf("verify current contracts: %w", err)
	}

	return nil
}

func (v sqlPublishFenceVerifier) publishMembershipScope(proof *contract.LeaseProof, observations []contract.Envelope) (collection.MembershipScope, error) {
	if len(observations) == 0 {
		return collection.MembershipScope{}, fmt.Errorf("verify publish fence: %w: observations are empty", ErrInvalidEnvelope)
	}

	definition, ok := v.jobs.Definition(collection.JobID{Provider: observations[0].Provider, Kind: collection.JobKind(proof.CollectionJobKind)})
	if !ok {
		return collection.MembershipScope{}, collection.ErrFenceLost
	}

	return collection.MembershipScopeFor(definition), nil
}

type publishFenceJob struct {
	provider          string
	collectionJobKind string
	definition        collection.JobContract
	jobSubject        string
}

func (v sqlPublishFenceVerifier) loadPublishFence(
	results pgx.BatchResults,
	proof *contract.LeaseProof,
) (publishFenceJob, error) {
	var (
		job      publishFenceJob
		jobClass string
	)

	err := results.QueryRow().Scan(&job.provider, &job.collectionJobKind, &jobClass, &job.jobSubject)

	if errors.Is(err, pgx.ErrNoRows) {
		return publishFenceJob{}, collection.ErrFenceLost
	}

	if err != nil {
		return publishFenceJob{}, fmt.Errorf("verify collection job fence: %w", err)
	}

	if job.collectionJobKind != proof.CollectionJobKind {
		return publishFenceJob{}, collection.ErrFenceLost
	}

	definition, ok := v.jobs.Definition(collection.JobID{Provider: contract.Provider(job.provider), Kind: collection.JobKind(job.collectionJobKind)})
	if !ok || string(definition.Class()) != jobClass || leaseSubjectMismatch(definition, job.jobSubject) {
		return publishFenceJob{}, collection.ErrFenceLost
	}

	job.definition = definition

	return job, nil
}

func leaseSubjectMismatch(job collection.JobContract, subject string) bool {
	return job.Membership() == collection.JobMembershipExactSubject && job.LeaseSubject() != "" && job.LeaseSubject() != subject
}

func verifyProjection(results pgx.BatchResults) error {
	var current int64

	err := results.QueryRow().Scan(&current)

	if errors.Is(err, pgx.ErrNoRows) {
		return collection.ErrProjectionStale
	}

	if err != nil {
		return fmt.Errorf("verify current collection projection: %w", err)
	}

	return nil
}

func publishTargetKeys(observations []contract.Envelope) (subjectKeys, kindNames []string) {
	subjectKeys = make([]string, len(observations))
	kindNames = make([]string, len(observations))

	for i := range observations {
		subjectKeys[i] = observations[i].SubjectKey
		kindNames[i] = string(observations[i].ObservationKind)
	}

	return subjectKeys, kindNames
}

func (v sqlPublishFenceVerifier) validatePublishObservations(job *publishFenceJob, observations []contract.Envelope) error {
	for i := range observations {
		if err := v.validatePublishObservation(job, &observations[i], i); err != nil {
			return fmt.Errorf("validate publish observation: %w", err)
		}
	}

	return nil
}

func (v sqlPublishFenceVerifier) validatePublishObservation(job *publishFenceJob, observation *contract.Envelope, index int) error {
	if job.provider != string(observation.Provider) ||
		!v.jobs.Allows(collection.JobID{Provider: observation.Provider, Kind: collection.JobKind(job.collectionJobKind)}, observation.ObservationKind) {
		return fmt.Errorf("verify collection job emission %d: %w", index, collection.ErrTargetDisabled)
	}

	if job.definition.Membership() == collection.JobMembershipExactSubject && observation.SubjectKey != job.jobSubject {
		return fmt.Errorf("verify collection job membership %d: %w", index, collection.ErrTargetDisabled)
	}

	if job.definition.Membership() != collection.JobMembershipExactSubject && job.definition.Membership() != collection.JobMembershipCurrentProjection {
		return fmt.Errorf("verify collection job membership %d: %w", index, collection.ErrTargetDisabled)
	}

	return nil
}

func verifyTargetsEnabled(results pgx.BatchResults) error {
	var allEnabled bool

	if err := results.QueryRow().Scan(&allEnabled); err != nil {
		return fmt.Errorf("verify collection targets: %w", err)
	}

	if !allEnabled {
		return fmt.Errorf("verify collection targets: %w", collection.ErrTargetDisabled)
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

	return r.runPreparedPublish(ctx, &prepared, r.completePublishTerminal)
}

func (r *Repository) PublishBatchAndDefer(
	ctx context.Context,
	input *PublishBatchInput,
	deferInput collection.DeferCollectionInput,
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

	return r.runPreparedPublish(ctx, &prepared, r.deferPublishTerminal(deferInput))
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
		return out, fmt.Errorf("publish prepared batch: %w", err)
	}

	out.EncodedBytes = len(prepared.observations)

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
		prepared.contracts,
	); err != nil {
		return fmt.Errorf("publish source observation batch: verify fence: %w", err)
	}

	if err := r.applyPublishFault(ctx, tx, faultAfterFenceVerify); err != nil {
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
