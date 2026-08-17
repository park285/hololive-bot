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
		return err
	}
	if fatal != nil {
		return nil
	}
	if err := validateResultShape(input, result); err != nil {
		return err
	}
	output := result.Output()
	observations := output.Observations()
	checkpoints := output.Checkpoints()
	if err := validateOutputBounds(output, observations, checkpoints); err != nil {
		return err
	}
	if err := validateResultEntries(input, registration, observations, checkpoints); err != nil {
		return err
	}
	if result.Kind() == collectutil.CollectComplete {
		if _, ok := result.PartialFailure(); ok {
			return invariantError("complete result contains a partial failure")
		}
		return nil
	}
	return validatePartialResult(registration, result, observations)
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

func validateOutputBounds(
	output collectutil.RunOutput,
	observations []contract.Envelope,
	checkpoints []sourceobservation.CheckpointEntry,
) error {
	if len(observations) != len(checkpoints) || len(observations) > sourceobservation.MaxPublishBatchSize {
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
	observations []contract.Envelope,
	checkpoints []sourceobservation.CheckpointEntry,
) error {
	spec := input.Spec()
	lease := input.Lease()
	job := registration.Contract()
	seen := make(map[string]struct{}, len(checkpoints))
	for index := range observations {
		if err := validateResultEntry(input, &spec, &lease, job, &observations[index], &checkpoints[index], seen); err != nil {
			return err
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
		return err
	}
	if err := validateEnvelopeLease(spec, lease, envelope); err != nil {
		return err
	}
	generation, err := input.Generation(envelope.ObservationKind)
	if err != nil || generation != envelope.ContractGeneration {
		return invariantError("observation contract generation is invalid")
	}
	if err := validateEnvelopeSubject(spec, job, envelope); err != nil {
		return err
	}
	return recordCheckpoint(envelope, checkpoint, seen)
}

func validateEnvelopeContract(job sourceobservation.JobContract, envelope *contract.Envelope) error {
	if envelope.Provider != job.ID().Provider || !job.Emits(envelope.ObservationKind) {
		return invariantError("observation lease or contract binding is invalid")
	}
	if envelope.ContractGeneration <= 0 {
		return invariantError("observation lease or contract binding is invalid")
	}
	return nil
}

func validateEnvelopeLease(spec *joblease.JobSpec, lease *contract.LeaseProof, envelope *contract.Envelope) error {
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

func validateEnvelopeSubject(spec *joblease.JobSpec, job sourceobservation.JobContract, envelope *contract.Envelope) error {
	if job.Membership() == sourceobservation.JobMembershipExactSubject && envelope.SubjectKey != spec.SubjectKey {
		return invariantError("observation subject does not match exact-subject lease")
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
		return invariantError("checkpoint does not match observation")
	}
	key := string(actual.Provider) + "\x00" + string(actual.ObservationKind) + "\x00" + actual.SubjectKey
	if _, ok := seen[key]; ok {
		return invariantError("checkpoint binding is duplicated")
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
		return err
	}
	failed := partial.FailedKinds()
	if len(failed) == 0 || len(observations) == 0 {
		return invariantError("partial result has no failed kind or output")
	}
	emitted := make([]contract.ObservationKind, 0, len(observations))
	for i := range observations {
		emitted = append(emitted, observations[i].ObservationKind)
	}
	return validateFailedKinds(registration.Contract(), failed, emitted)
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
