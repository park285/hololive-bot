package collectorruntime

import (
	"fmt"
	"slices"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collection"
)

func ValidateCollectResult(
	input *collection.RunInput,
	registration RegisteredRunner,
	result *collection.CollectResult,
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

	if result.Kind() == collection.CollectComplete {
		return validateCompleteResult(result)
	}

	if err := validatePartialResult(registration, result, output); err != nil {
		return fmt.Errorf("validate partial result: %w", err)
	}

	return nil
}

func validateCompleteResult(result *collection.CollectResult) error {
	if _, ok := result.PartialFailure(); ok {
		return invariantError("complete result contains a partial failure")
	}

	return nil
}

func validateFatalResult(result *collection.CollectResult, fatal error) error {
	if fatal != nil && !result.IsZero() {
		return invariantError("fatal collection returned a non-zero result")
	}

	return nil
}

func validateResultShape(input *collection.RunInput, result *collection.CollectResult) error {
	if input == nil || !validCollectResultKind(result.Kind()) {
		return invariantError("collection result kind is invalid")
	}

	return nil
}

func validCollectResultKind(kind collection.CollectResultKind) bool {
	return kind == collection.CollectComplete || kind == collection.CollectPartial
}

func validateOutputBounds(output collection.RunOutput) error {
	if output.ObservationCount() > collection.MaxPublishBatchSize {
		return invariantError("collection output bounds are invalid")
	}

	if output.CollectionLatency() < 0 || output.CollectionLatency() > collection.MaxCollectionLatency {
		return invariantError("collection output bounds are invalid")
	}

	return nil
}

func validateResultEntries(
	input *collection.RunInput,
	registration RegisteredRunner,
	output collection.RunOutput,
) error {
	subject := input.Subject()
	lease := input.Lease()
	job := registration.Contract()
	seen := make(map[string]struct{}, output.ObservationCount())

	for index := range output.ObservationCount() {
		envelope := output.ObservationMetadata(index)

		if err := validateResultEntry(input, subject, &lease, job, &envelope, seen); err != nil {
			return fmt.Errorf("validate result entry: %w", err)
		}
	}

	return nil
}

func validateResultEntry(
	input *collection.RunInput,
	subject string,
	lease *contract.LeaseProof,
	job collection.JobContract,
	envelope *collection.ObservationMetadata,
	seen map[string]struct{},
) error {
	if err := validateEnvelopeContract(job, envelope); err != nil {
		return fmt.Errorf("validate envelope contract: %w", err)
	}

	if err := validateEnvelopeLease(lease, envelope); err != nil {
		return fmt.Errorf("validate envelope lease: %w", err)
	}

	generation, err := input.Generation(envelope.ObservationKind)
	if err != nil || generation != envelope.ContractGeneration {
		return invariantError("observation contract generation is invalid")
	}

	if err := validateEnvelopeSubject(subject, job, envelope); err != nil {
		return fmt.Errorf("validate envelope subject: %w", err)
	}

	if err := recordCheckpointBinding(envelope, seen); err != nil {
		return fmt.Errorf("record checkpoint binding: %w", err)
	}

	return nil
}

func validateEnvelopeContract(job collection.JobContract, envelope *collection.ObservationMetadata) error {
	if envelope.Provider != job.ID().Provider || !job.Emits(envelope.ObservationKind) {
		return invariantError("observation lease or contract binding is invalid")
	}

	if envelope.ContractGeneration <= 0 {
		return invariantError("observation lease or contract binding is invalid")
	}

	return nil
}

// validateEnvelopeLease는 관측 lease가 입력 lease와 같은지 확인합니다. 입력 lease의 job key·kind 결합은
// target snapshot 적재와 실행 입력 생성이 이미 검증했습니다.
func validateEnvelopeLease(lease *contract.LeaseProof, envelope *collection.ObservationMetadata) error {
	if envelope.Lease != *lease {
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

func validateEnvelopeSubject(subject string, job collection.JobContract, envelope *collection.ObservationMetadata) error {
	if job.Membership() == collection.JobMembershipExactSubject && envelope.SubjectKey != subject {
		return invariantError("observation subject does not match exact-subject lease")
	}

	return nil
}

// recordCheckpointBinding은 관측에서 파생될 체크포인트 바인딩이 배치 안에서 유일한지 확인합니다.
func recordCheckpointBinding(envelope *collection.ObservationMetadata, seen map[string]struct{}) error {
	key := string(envelope.Provider) + "\x00" + string(envelope.ObservationKind) + "\x00" + envelope.SubjectKey
	if _, ok := seen[key]; ok {
		return invariantError("checkpoint binding is duplicated")
	}

	seen[key] = struct{}{}

	return nil
}

func validatePartialResult(
	registration RegisteredRunner,
	result *collection.CollectResult,
	output collection.RunOutput,
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

func validatedPartialFailure(result *collection.CollectResult) (*collection.PartialFailure, error) {
	partial, ok := result.PartialFailure()
	if !ok || partial.Cause() == nil || !collection.PartialFailureClassAllowed(collecterr.ClassOf(partial.Cause())) {
		return nil, invariantError("partial result failure is invalid")
	}

	return partial, nil
}

func validateFailedKinds(
	job collection.JobContract,
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
