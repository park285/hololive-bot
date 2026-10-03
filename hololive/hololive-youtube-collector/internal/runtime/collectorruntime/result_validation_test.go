package collectorruntime

import (
	"errors"
	"testing"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collectutil"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/sourceobservation"
)

const testOtherBinding = "other"

func TestValidateCollectResultChecksMetadataBindings(t *testing.T) {
	input, registration, envelope := resultValidationFixture(t)
	checkpoint := collectutil.Checkpoint(&envelope)

	checkpoint.Cursor = []byte(`{"next":"page"}`)

	if err := validateFixtureOutput(t, &input, registration, []contract.Envelope{envelope}, []sourceobservation.CheckpointEntry{checkpoint}); err != nil {
		t.Fatalf("valid result rejected: %v", err)
	}

	for name, mutate := range envelopeBindingMutations() {
		t.Run(name, func(t *testing.T) {
			changed := envelope
			mutate(&changed)

			changedCheckpoint := collectutil.Checkpoint(&changed)
			err := validateFixtureOutput(t, &input, registration, []contract.Envelope{changed}, []sourceobservation.CheckpointEntry{changedCheckpoint})
			requireInvariantFailure(t, err)
		})
	}

	for name, mutate := range checkpointBindingMutations() {
		t.Run(name, func(t *testing.T) {
			changed := checkpoint
			mutate(&changed)

			err := validateFixtureOutput(t, &input, registration, []contract.Envelope{envelope}, []sourceobservation.CheckpointEntry{changed})
			requireInvariantFailure(t, err)
		})
	}

	t.Run("duplicate binding", func(t *testing.T) {
		err := validateFixtureOutput(t, &input, registration,
			[]contract.Envelope{envelope, envelope}, []sourceobservation.CheckpointEntry{checkpoint, checkpoint})
		requireInvariantFailure(t, err)
	})
}

func envelopeBindingMutations() map[string]func(*contract.Envelope) {
	return map[string]func(*contract.Envelope){
		"provider":          func(e *contract.Envelope) { e.Provider = contract.ProviderHolodex },
		"kind":              func(e *contract.Envelope) { e.ObservationKind = contract.KindVideoList },
		"generation":        func(e *contract.Envelope) { e.ContractGeneration++ },
		"zero generation":   func(e *contract.Envelope) { e.ContractGeneration = 0 },
		"subject":           func(e *contract.Envelope) { e.SubjectKey = "UC_OTHER" },
		"collector":         func(e *contract.Envelope) { e.CollectorInstance = testOtherBinding },
		"schedule":          func(e *contract.Envelope) { e.ScheduledFor = e.ScheduledFor.Add(time.Second) },
		"lease job":         func(e *contract.Envelope) { e.Lease.JobKey = testOtherBinding },
		"lease owner":       func(e *contract.Envelope) { e.Lease.OwnerInstance = testOtherBinding },
		"lease fence":       func(e *contract.Envelope) { e.Lease.FenceEpoch++ },
		"lease projection":  func(e *contract.Envelope) { e.Lease.ProjectionGeneration++ },
		"lease collection":  func(e *contract.Envelope) { e.Lease.CollectionJobKind = testOtherBinding },
		"lease scheduledAt": func(e *contract.Envelope) { e.Lease.ScheduledFor = e.Lease.ScheduledFor.Add(time.Second) },
	}
}

func checkpointBindingMutations() map[string]func(*sourceobservation.CheckpointEntry) {
	return map[string]func(*sourceobservation.CheckpointEntry){
		"checkpoint provider":   func(c *sourceobservation.CheckpointEntry) { c.Provider = contract.ProviderHolodex },
		"checkpoint kind":       func(c *sourceobservation.CheckpointEntry) { c.ObservationKind = contract.KindVideoList },
		"checkpoint subject":    func(c *sourceobservation.CheckpointEntry) { c.SubjectKey = "UC_OTHER" },
		"checkpoint scope":      func(c *sourceobservation.CheckpointEntry) { c.ScopeSHA256 = testOtherBinding },
		"checkpoint generation": func(c *sourceobservation.CheckpointEntry) { c.ContractGeneration++ },
		"checkpoint key":        func(c *sourceobservation.CheckpointEntry) { c.LastObservationKey = testOtherBinding },
		"checkpoint evidence":   func(c *sourceobservation.CheckpointEntry) { c.LastEvidenceSHA256 = testOtherBinding },
		"checkpoint schedule":   func(c *sourceobservation.CheckpointEntry) { c.LastScheduledFor = c.LastScheduledFor.Add(time.Second) },
		"checkpoint continuity": func(c *sourceobservation.CheckpointEntry) { c.Continuity = contract.ContinuityNotApplicable },
	}
}

func resultValidationFixture(t *testing.T) (collectutil.RunInput, RegisteredRunner, contract.Envelope) {
	t.Helper()

	var fatal []error

	executor, spec := newExecutorFixture(t, stubJob(contract.ProviderYouTubeJS, testCommunityJobKind), &fatal)

	lease, err := executor.acquireLease(t.Context(), spec)
	if err != nil {
		t.Fatal(err)
	}

	proof := lease.Proof()
	registration, _ := executor.registry.Lookup(spec.Provider, spec.CollectionJobKind)

	input, err := executor.buildRunInput(t.Context(), registration, spec, &proof)
	if err != nil {
		t.Fatal(err)
	}

	generation, err := input.Generation(contract.KindCommunityPage)
	if err != nil {
		t.Fatal(err)
	}

	return input, registration, contract.Envelope{
		Provider: spec.Provider, ObservationKind: contract.KindCommunityPage,
		SubjectKey: spec.SubjectKey, ObservationKey: "observation", ContractGeneration: generation,
		ScheduledFor: proof.ScheduledFor, ScopeSHA256: "scope", EvidenceSHA256: "evidence",
		Completeness: contract.CompletenessComplete, Continuity: contract.ContinuityContiguous,
		CollectorInstance: proof.OwnerInstance, Lease: proof, Payload: []byte(`{"value":1}`),
	}
}

func validateFixtureOutput(
	t *testing.T,
	input *collectutil.RunInput,
	registration RegisteredRunner,
	observations []contract.Envelope,
	checkpoints []sourceobservation.CheckpointEntry,
) error {
	t.Helper()

	output, err := collectutil.NewRunOutput(observations, checkpoints, time.Second)
	if err != nil {
		t.Fatal(err)
	}

	result, err := collectutil.NewCompleteResult(output)
	if err != nil {
		t.Fatal(err)
	}

	return ValidateCollectResult(input, registration, &result, nil)
}

func requireInvariantFailure(t *testing.T, err error) {
	t.Helper()

	if _, ok := errors.AsType[*collecterr.Error](err); !ok ||
		collecterr.CodeOf(err) != collecterr.Internal || collecterr.ClassOf(err) != collecterr.ClassInternal ||
		!fatalCollectionError(err) {
		t.Fatalf("validation error = %v, want identifiable fatal internal invariant", err)
	}
}

func TestValidateFatalResultPreservesCollectionFailureBoundary(t *testing.T) {
	t.Parallel()

	cause := errors.New("provider failed")
	if err := ValidateCollectResult(nil, RegisteredRunner{}, nil, cause); err != nil {
		t.Fatalf("fatal with no result was treated as a validation failure: %v", err)
	}

	result, err := collectutil.NewCompleteResult(collectutil.RunOutput{})
	if err != nil {
		t.Fatal(err)
	}

	requireInvariantFailure(t, ValidateCollectResult(nil, RegisteredRunner{}, &result, cause))
	requireInvariantFailure(t, ValidateCollectResult(nil, RegisteredRunner{}, nil, nil))
}
