package sourceobservation

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/dbx"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collection"
)

type sqlPublishFenceVerifier struct {
	jobs collection.JobContractSet
}

// publishVerificationBatchSender는 pgx 트랜잭션의 파이프라인 전송 능력이다. 발행 트랜잭션은 항상
// pgxpool에서 시작하므로 이 능력이 없으면 순차 경로로 바꾸지 않고 오류로 드러낸다.
type publishVerificationBatchSender interface {
	SendBatch(ctx context.Context, batch *pgx.Batch) pgx.BatchResults
}

// Verify는 guard·fence·membership·target·contract 조회를 한 번의 왕복으로 보냅니다.
// 서버 실행과 결과 판정 순서는 projection guard(share) → lease(FOR UPDATE) → job membership → target →
// contract이며, collector의 acquire·CompleteCurrent와 같은 잠금 순서다. Guard 대기 뒤의 문장은 새 snapshot에서
// 평가되므로 API 전환 중 기다린 publish가 사라진 generation을 보고 거절되지 않는다. 앞선 판정이 실패하면
// 이미 획득한 뒤쪽 잠금도 트랜잭션과 함께 롤백됩니다.
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

	// 판정이 앞에서 끝나도 남은 응답을 회수해 연결을 다음 문장에 쓸 수 있게 한다.
	defer func() {
		if closeErr := results.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("close publish verification batch: %w", closeErr))
		}
	}()

	return v.verifyPublishResults(results, proof, observations)
}

// publishVerificationBatch는 Verify의 판정 순서대로 guard·fence·membership·target·contract 조회를 한 batch에 쌓는다.
func publishVerificationBatch(
	proof *contract.LeaseProof,
	scope collection.MembershipScope,
	observations []contract.Envelope,
	contracts []byte,
) *pgx.Batch {
	subjects, kinds := publishTargetKeys(observations)
	membershipQuery, membershipArgs := collection.LeaseMembershipQuery(proof, scope)
	batch := &pgx.Batch{}

	batch.Queue(sqlProjectionCurrent)
	batch.Queue(
		sqlPublishFence,
		proof.JobKey,
		proof.OwnerInstance,
		proof.FenceEpoch,
		proof.ProjectionGeneration,
		proof.ScheduledFor,
	)
	batch.Queue(membershipQuery, membershipArgs...)
	batch.Queue(sqlTargetEnabled, proof.ProjectionGeneration, subjects, kinds)
	batch.Queue(sqlContractBatchCurrent, string(contracts))

	return batch
}

// verifyPublishResults는 publishVerificationBatch가 쌓은 순서대로 응답을 읽어 판정한다.
func (v sqlPublishFenceVerifier) verifyPublishResults(
	results pgx.BatchResults,
	proof *contract.LeaseProof,
	observations []contract.Envelope,
) error {
	// 소유 손실이 projection 부재보다 우선한다. 다른 소유자의 lease를 superseded로 해제하려 하지 않게 한다.
	projectionErr := verifyProjection(results)

	job, err := v.loadPublishFence(results, proof)
	if err != nil {
		return fmt.Errorf("load publish fence: %w", err)
	}

	if projectionErr != nil {
		return fmt.Errorf("verify projection: %w", projectionErr)
	}

	if err := collection.ScanLeaseMembership(results.QueryRow()); err != nil {
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

// publishMembershipScope는 lease 행을 읽기 전에 같은 batch로 보낼 membership 범위를 컴파일된 계약에서 만든다.
// Provider는 관측에서 가져오며, 실제 lease 행의 provider·job 종류와 다르면 loadPublishFence가 fence 손실로 거절한다.
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

// leaseSubjectMismatch는 고정 lease subject를 가진 EXACT_SUBJECT job의 lease 행 subject가 계약과 다른지 확인합니다.
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

	out, err := r.runPreparedPublish(ctx, &prepared, r.completePublishTerminal)
	if err != nil {
		return out, fmt.Errorf("run prepared publish: %w", err)
	}

	return out, nil
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

	out, err := r.runPreparedPublish(ctx, &prepared, r.deferPublishTerminal(deferInput))
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
