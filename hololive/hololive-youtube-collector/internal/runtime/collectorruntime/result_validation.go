package collectorruntime

import (
	"errors"
	"fmt"
	"slices"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/service/youtube/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collectutil"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/joblease"
)

var errInvalidPartialFailure = errors.New("partial result failure is invalid")

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
	observations := output.Observations()
	checkpoints := output.Checkpoints()

	if err := validateOutputBounds(output, observations, checkpoints); err != nil {
		return fmt.Errorf("validate output bounds: %w", err)
	}

	if err := validateResultEntries(input, registration, observations, checkpoints); err != nil {
		return fmt.Errorf("validate result entries: %w", err)
	}

	if result.Kind() == collectutil.CollectComplete {
		return errors.Join(validateCompleteResult(result))
	}

	if err := validatePartialResult(registration, result, observations); err != nil {
		return fmt.Errorf("validate partial result: %w", err)
	}

	return nil
}

func validateCompleteResult(result *collectutil.CollectResult) error {
	if _, ok := result.PartialFailure(); ok {
		return errors.Join(wrappedInvariantError("complete result contains a partial failure"))
	}

	return nil
}

func validateFatalResult(result *collectutil.CollectResult, fatal error) error {
	if fatal != nil && !result.IsZero() {
		if err := invariantError("fatal collection returned a non-zero result"); err != nil {
			return fmt.Errorf("invariant error: %w", err)
		}

		return nil
	}

	return nil
}

func validateResultShape(input *collectutil.RunInput, result *collectutil.CollectResult) error {
	if input == nil || !validCollectResultKind(result.Kind()) {
		if err := invariantError("collection result kind is invalid"); err != nil {
			return fmt.Errorf("invariant error: %w", err)
		}

		return nil
	}

	return nil
}

func validCollectResultKind(kind collectutil.CollectResultKind) bool {
	return kind == collectutil.CollectComplete || kind == collectutil.CollectPartial
}

func validateOutputBounds(
	output collectutil.RunOutput,
	observations []contract.Envelope,
	checkpoints []sourceobservation.CheckpointEntry,
) error {
	if len(observations) != len(checkpoints) || len(observations) > sourceobservation.MaxPublishBatchSize {
		if err := invariantError("collection output bounds are invalid"); err != nil {
			return fmt.Errorf("invariant error: %w", err)
		}

		return nil
	}

	if output.CollectionLatency() < 0 || output.CollectionLatency() > sourceobservation.MaxCollectionLatency {
		if err := invariantError("collection output bounds are invalid"); err != nil {
			return fmt.Errorf("invariant error: %w", err)
		}

		return nil
	}

	return nil
}

func validateResultEntries(
	input *collectutil.RunInput,
	registration RegisteredRunner,
	observations []contract.Envelope,
	checkpoints []sourceobservation.CheckpointEntry,
) error {
	spec := input.Spec()
	lease := input.Lease()
	job := registration.Contract()
	seen := make(map[string]struct{}, len(checkpoints))

	for index := range observations {
		if err := validateResultEntry(input, &spec, &lease, job, &observations[index], &checkpoints[index], seen); err != nil {
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
	envelope *contract.Envelope,
	checkpoint *sourceobservation.CheckpointEntry,
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
		if err := invariantError("observation contract generation is invalid"); err != nil {
			return fmt.Errorf("invariant error: %w", err)
		}

		return nil
	}

	if err := validateEnvelopeSubject(spec, job, envelope); err != nil {
		return fmt.Errorf("validate envelope subject: %w", err)
	}

	if err := recordCheckpoint(envelope, checkpoint, seen); err != nil {
		return fmt.Errorf("record checkpoint: %w", err)
	}

	return nil
}

func validateEnvelopeContract(job sourceobservation.JobContract, envelope *contract.Envelope) error {
	if envelope.Provider != job.ID().Provider || !job.Emits(envelope.ObservationKind) {
		if err := invariantError("observation lease or contract binding is invalid"); err != nil {
			return fmt.Errorf("invariant error: %w", err)
		}

		return nil
	}

	if envelope.ContractGeneration <= 0 {
		if err := invariantError("observation lease or contract binding is invalid"); err != nil {
			return fmt.Errorf("invariant error: %w", err)
		}

		return nil
	}

	return nil
}

func validateEnvelopeLease(spec *joblease.JobSpec, lease *contract.LeaseProof, envelope *contract.Envelope) error {
	if envelope.Lease != *lease {
		return errors.Join(wrappedInvariantError("observation lease or contract binding is invalid"))
	}

	if envelope.Lease.JobKey != spec.JobKey || envelope.Lease.CollectionJobKind != spec.CollectionJobKind {
		return errors.Join(wrappedInvariantError("observation lease or contract binding is invalid"))
	}

	if envelope.CollectorInstance != lease.OwnerInstance || envelope.Lease.ProjectionGeneration != lease.ProjectionGeneration {
		return errors.Join(wrappedInvariantError("observation lease or contract binding is invalid"))
	}

	if !envelope.ScheduledFor.Equal(lease.ScheduledFor) {
		return errors.Join(wrappedInvariantError("observation lease or contract binding is invalid"))
	}

	return nil
}

func validateEnvelopeSubject(spec *joblease.JobSpec, job sourceobservation.JobContract, envelope *contract.Envelope) error {
	if job.Membership() == sourceobservation.JobMembershipExactSubject && envelope.SubjectKey != spec.SubjectKey {
		if err := invariantError("observation subject does not match exact-subject lease"); err != nil {
			return fmt.Errorf("invariant error: %w", err)
		}

		return nil
	}

	return nil
}

func recordCheckpoint(
	envelope *contract.Envelope,
	actual *sourceobservation.CheckpointEntry,
	seen map[string]struct{},
) error {
	expected := collectutil.Checkpoint(envelope)
	if !checkpointMatches(&expected, actual) {
		if err := invariantError("checkpoint does not match observation"); err != nil {
			return fmt.Errorf("invariant error: %w", err)
		}

		return nil
	}

	key := string(actual.Provider) + "\x00" + string(actual.ObservationKind) + "\x00" + actual.SubjectKey
	if _, ok := seen[key]; ok {
		if err := invariantError("checkpoint binding is duplicated"); err != nil {
			return fmt.Errorf("invariant error: %w", err)
		}

		return nil
	}

	seen[key] = struct{}{}

	return nil
}

func checkpointMatches(expected, actual *sourceobservation.CheckpointEntry) bool {
	return expected.Provider == actual.Provider &&
		expected.ObservationKind == actual.ObservationKind &&
		expected.SubjectKey == actual.SubjectKey &&
		expected.ScopeSHA256 == actual.ScopeSHA256 &&
		expected.ContractGeneration == actual.ContractGeneration &&
		expected.LastObservationKey == actual.LastObservationKey &&
		expected.LastEvidenceSHA256 == actual.LastEvidenceSHA256 &&
		expected.LastScheduledFor.Equal(actual.LastScheduledFor) &&
		expected.Continuity == actual.Continuity
}

func validatePartialResult(
	registration RegisteredRunner,
	result *collectutil.CollectResult,
	observations []contract.Envelope,
) error {
	partial, err := validatedPartialFailure(result)
	if err != nil {
		return fmt.Errorf("validated partial failure: %w", err)
	}

	failed := partial.FailedKinds()
	if len(failed) == 0 || len(observations) == 0 {
		if err := invariantError("partial result has no failed kind or output"); err != nil {
			return fmt.Errorf("invariant error: %w", err)
		}

		return nil
	}

	emitted := make([]contract.ObservationKind, 0, len(observations))
	for i := range observations {
		emitted = append(emitted, observations[i].ObservationKind)
	}

	if err := validateFailedKinds(registration.Contract(), failed, emitted); err != nil {
		return fmt.Errorf("validate failed kinds: %w", err)
	}

	return nil
}

func validatedPartialFailure(result *collectutil.CollectResult) (*collectutil.PartialFailure, error) {
	partial, ok := result.PartialFailure()
	if !ok || partial.Cause() == nil || !collectutil.PartialFailureClassAllowed(collecterr.ClassOf(partial.Cause())) {
		if err := invariantError("partial result failure is invalid"); err != nil {
			return nil, fmt.Errorf("invariant error: %w", err)
		}

		return nil, errInvalidPartialFailure
	}

	return partial, nil
}

func validateFailedKinds(
	job sourceobservation.JobContract,
	failed, emitted []contract.ObservationKind,
) error {
	for _, kind := range failed {
		if !job.Emits(kind) || slices.Contains(emitted, kind) {
			if err := invariantError("partial failed kind is outside the job contract or overlaps output"); err != nil {
				return fmt.Errorf("invariant error: %w", err)
			}

			return nil
		}
	}

	return nil
}

func invariantError(message string) error {
	return collecterr.Wrap(collecterr.Internal, collecterr.ClassInternal, fmt.Errorf("collection_internal_invariant: %s", message))
}

func wrappedInvariantError(message string) error {
	return fmt.Errorf("invariant error: %w", invariantError(message))
}
