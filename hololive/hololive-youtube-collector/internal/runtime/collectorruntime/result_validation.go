package collectorruntime

import (
	"fmt"
	"slices"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/service/youtube/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collectutil"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/joblease"
)

func ValidateCollectResult(
	input *collectutil.RunInput,
	registration RegisteredRunner,
	result *collectutil.CollectResult,
	fatal error,
) error {
	if err := validateFatalResult(result, fatal); err != nil {
		return fmt.Errorf("validate fatal result: %w", err)
	}

	if fatal != nil {
		//nolint:nilerr // fatal은 이 함수가 낸 오류가 아니라 검증 대상 입력이고 호출자가 따로 처리한다. 전파하면 검증 실패와 수집 실패가 뒤섞인다.
		return nil
	}

	if err := validateResultShape(input, result); err != nil {
		return fmt.Errorf("validate result shape: %w", err)
	}

	output := result.Output()

	if err := validateOutputBounds(output); err != nil {
		return fmt.Errorf("validate output bounds: %w", err)
	}

	if err := validateResultEntries(input, registration, output); err != nil {
		return fmt.Errorf("validate result entries: %w", err)
	}

	if result.Kind() == collectutil.CollectComplete {
		return validateCompleteResult(result)
	}

	if err := validatePartialResult(registration, result, output); err != nil {
		return fmt.Errorf("validate partial result: %w", err)
	}

	return nil
}

func validateCompleteResult(result *collectutil.CollectResult) error {
	if _, ok := result.PartialFailure(); ok {
		return invariantError("complete result contains a partial failure")
	}

	return nil
}

func validateFatalResult(result *collectutil.CollectResult, fatal error) error {
	if fatal != nil && !result.IsZero() {
		return invariantError("fatal collection returned a non-zero result")
	}

	return nil
}

func validateResultShape(input *collectutil.RunInput, result *collectutil.CollectResult) error {
	if input == nil || !validCollectResultKind(result.Kind()) {
		return invariantError("collection result kind is invalid")
	}

	return nil
}

func validCollectResultKind(kind collectutil.CollectResultKind) bool {
	return kind == collectutil.CollectComplete || kind == collectutil.CollectPartial
}

func validateOutputBounds(output collectutil.RunOutput) error {
	if output.ObservationCount() != output.CheckpointCount() || output.ObservationCount() > sourceobservation.MaxPublishBatchSize {
		return invariantError("collection output bounds are invalid")
	}

	if output.CollectionLatency() < 0 || output.CollectionLatency() > sourceobservation.MaxCollectionLatency {
		return invariantError("collection output bounds are invalid")
	}

	return nil
}

func validateResultEntries(
	input *collectutil.RunInput,
	registration RegisteredRunner,
	output collectutil.RunOutput,
) error {
	spec := input.Spec()
	lease := input.Lease()
	job := registration.Contract()
	seen := make(map[string]struct{}, output.CheckpointCount())

	for index := range output.ObservationCount() {
		envelope := output.ObservationMetadata(index)
		checkpoint := output.CheckpointMetadata(index)

		if err := validateResultEntry(input, &spec, &lease, job, &envelope, &checkpoint, seen); err != nil {
			return fmt.Errorf("validate result entry: %w", err)
		}
	}

	return nil
}

func validateResultEntry(
	input *collectutil.RunInput,
	spec *joblease.JobSpec,
	lease *contract.LeaseProof,
	job sourceobservation.JobContract,
	envelope *collectutil.ObservationMetadata,
	checkpoint *collectutil.CheckpointMetadata,
	seen map[string]struct{},
) error {
	if err := validateEnvelopeContract(job, envelope); err != nil {
		return fmt.Errorf("validate envelope contract: %w", err)
	}

	if err := validateEnvelopeLease(spec, lease, envelope); err != nil {
		return fmt.Errorf("validate envelope lease: %w", err)
	}

	generation, err := input.Generation(envelope.ObservationKind)
	if err != nil || generation != envelope.ContractGeneration {
		return invariantError("observation contract generation is invalid")
	}

	if err := validateEnvelopeSubject(spec, job, envelope); err != nil {
		return fmt.Errorf("validate envelope subject: %w", err)
	}

	if err := recordCheckpoint(envelope, checkpoint, seen); err != nil {
		return fmt.Errorf("record checkpoint: %w", err)
	}

	return nil
}

func validateEnvelopeContract(job sourceobservation.JobContract, envelope *collectutil.ObservationMetadata) error {
	if envelope.Provider != job.ID().Provider || !job.Emits(envelope.ObservationKind) {
		return invariantError("observation lease or contract binding is invalid")
	}

	if envelope.ContractGeneration <= 0 {
		return invariantError("observation lease or contract binding is invalid")
	}

	return nil
}

func validateEnvelopeLease(spec *joblease.JobSpec, lease *contract.LeaseProof, envelope *collectutil.ObservationMetadata) error {
	if envelope.Lease != *lease {
		return invariantError("observation lease or contract binding is invalid")
	}

	if envelope.Lease.JobKey != spec.JobKey || envelope.Lease.CollectionJobKind != spec.CollectionJobKind {
		return invariantError("observation lease or contract binding is invalid")
	}

	if envelope.CollectorInstance != lease.OwnerInstance || envelope.Lease.ProjectionGeneration != lease.ProjectionGeneration {
		return invariantError("observation lease or contract binding is invalid")
	}

	if !envelope.ScheduledFor.Equal(lease.ScheduledFor) {
		return invariantError("observation lease or contract binding is invalid")
	}

	return nil
}

func validateEnvelopeSubject(spec *joblease.JobSpec, job sourceobservation.JobContract, envelope *collectutil.ObservationMetadata) error {
	if job.Membership() == sourceobservation.JobMembershipExactSubject && envelope.SubjectKey != spec.SubjectKey {
		return invariantError("observation subject does not match exact-subject lease")
	}

	return nil
}

func recordCheckpoint(
	envelope *collectutil.ObservationMetadata,
	actual *collectutil.CheckpointMetadata,
	seen map[string]struct{},
) error {
	if !checkpointMatches(envelope, actual) {
		return invariantError("checkpoint does not match observation")
	}

	key := string(actual.Provider) + "\x00" + string(actual.ObservationKind) + "\x00" + actual.SubjectKey
	if _, ok := seen[key]; ok {
		return invariantError("checkpoint binding is duplicated")
	}

	seen[key] = struct{}{}

	return nil
}

func checkpointMatches(expected *collectutil.ObservationMetadata, actual *collectutil.CheckpointMetadata) bool {
	return expected.Provider == actual.Provider &&
		expected.ObservationKind == actual.ObservationKind &&
		expected.SubjectKey == actual.SubjectKey &&
		expected.ScopeSHA256 == actual.ScopeSHA256 &&
		expected.ContractGeneration == actual.ContractGeneration &&
		expected.ObservationKey == actual.LastObservationKey &&
		expected.EvidenceSHA256 == actual.LastEvidenceSHA256 &&
		expected.ScheduledFor.Equal(actual.LastScheduledFor) &&
		expected.Continuity == actual.Continuity
}

func validatePartialResult(
	registration RegisteredRunner,
	result *collectutil.CollectResult,
	output collectutil.RunOutput,
) error {
	partial, err := validatedPartialFailure(result)
	if err != nil {
		return fmt.Errorf("validated partial failure: %w", err)
	}

	failed := partial.FailedKinds()
	if len(failed) == 0 || output.Empty() {
		return invariantError("partial result has no failed kind or output")
	}

	emitted := make([]contract.ObservationKind, 0, output.ObservationCount())
	for i := range output.ObservationCount() {
		emitted = append(emitted, output.ObservationMetadata(i).ObservationKind)
	}

	if err := validateFailedKinds(registration.Contract(), failed, emitted); err != nil {
		return fmt.Errorf("validate failed kinds: %w", err)
	}

	return nil
}

func validatedPartialFailure(result *collectutil.CollectResult) (*collectutil.PartialFailure, error) {
	partial, ok := result.PartialFailure()
	if !ok || partial.Cause() == nil || !collectutil.PartialFailureClassAllowed(collecterr.ClassOf(partial.Cause())) {
		return nil, invariantError("partial result failure is invalid")
	}

	return partial, nil
}

func validateFailedKinds(
	job sourceobservation.JobContract,
	failed, emitted []contract.ObservationKind,
) error {
	for _, kind := range failed {
		if !job.Emits(kind) || slices.Contains(emitted, kind) {
			return invariantError("partial failed kind is outside the job contract or overlaps output")
		}
	}

	return nil
}

func invariantError(message string) error {
	return collecterr.Wrap(collecterr.Internal, collecterr.ClassInternal, fmt.Errorf("collection_internal_invariant: %s", message))
}
