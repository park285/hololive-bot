package collection

import (
	"context"
	"encoding/json/jsontext"
	"fmt"
	"slices"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
)

type JobRunner interface {
	JobID() JobID
	Collect(ctx context.Context, input *RunInput) (CollectResult, error)
}

type ContractSnapshot struct {
	generations map[contract.ObservationKind]int64
}

func NewContractSnapshot(required []contract.ObservationKind, values map[contract.ObservationKind]int64) (ContractSnapshot, error) {
	snapshot := ContractSnapshot{generations: make(map[contract.ObservationKind]int64, len(required))}
	seen := make(map[contract.ObservationKind]struct{}, len(required))

	for _, kind := range required {
		if !kind.Valid() {
			return ContractSnapshot{}, fmt.Errorf("build contract snapshot: invalid observation kind %q", kind)
		}

		if _, ok := seen[kind]; ok {
			return ContractSnapshot{}, fmt.Errorf("build contract snapshot: duplicate observation kind %q", kind)
		}

		seen[kind] = struct{}{}

		generation := values[kind]

		if generation <= 0 {
			return ContractSnapshot{}, fmt.Errorf("build contract snapshot: generation is missing for %q", kind)
		}

		snapshot.generations[kind] = generation
	}

	return snapshot, nil
}

func (s ContractSnapshot) Generation(kind contract.ObservationKind) (int64, error) {
	generation := s.generations[kind]
	if generation <= 0 {
		return 0, collecterr.New(contract.ErrorInternalInvariant, contract.ClassInternal, "observation contract generation is missing")
	}

	return generation, nil
}

func (s ContractSnapshot) ValidateKinds(kinds []contract.ObservationKind) error {
	for _, kind := range kinds {
		if _, err := s.Generation(kind); err != nil {
			return fmt.Errorf("generation: %w", err)
		}
	}

	return nil
}

type RunInput struct {
	job                     JobContract
	subject                 string
	lease                   contract.LeaseProof
	contracts               ContractSnapshot
	targets                 TargetSnapshot
	maxPages                int
	maxSuccessResponseBytes int
}

type RunOutput struct {
	observations      []contract.Envelope
	cursors           []jsontext.Value
	collectionLatency time.Duration
}

func NewRunInput(
	job JobContract,
	subject string,
	lease *contract.LeaseProof,
	contracts ContractSnapshot,
	targets TargetSnapshot,
	maxPages, maxSuccessResponseBytes int,
) (RunInput, error) {
	if lease == nil {
		return RunInput{}, invalidRunInputError()
	}

	if err := validateRunInputIdentity(job, lease, maxPages, maxSuccessResponseBytes); err != nil {
		return RunInput{}, fmt.Errorf("validate run input identity: %w", err)
	}

	if err := contracts.ValidateKinds(job.Emissions()); err != nil {
		return RunInput{}, fmt.Errorf("validate kinds: %w", err)
	}

	if targets.Generation() != lease.ProjectionGeneration || targets.Membership() != job.Membership() {
		return RunInput{}, collecterr.New(contract.ErrorInternalInvariant, contract.ClassInternal, "collection target snapshot identity does not match")
	}

	if subjectMismatch(job, subject, &targets) {
		return RunInput{}, collecterr.New(contract.ErrorInternalInvariant, contract.ClassInternal, "collection run subject does not match")
	}

	if err := targets.ValidateRequested(job.RequestedKinds()); err != nil {
		return RunInput{}, fmt.Errorf("validate requested: %w", err)
	}

	return RunInput{
		job: job, subject: subject, lease: *lease, contracts: contracts,
		targets: targets.Clone(), maxPages: maxPages, maxSuccessResponseBytes: maxSuccessResponseBytes,
	}, nil
}

func validateRunInputIdentity(
	job JobContract,
	lease *contract.LeaseProof,
	maxPages, maxSuccessResponseBytes int,
) error {
	if err := job.Validate(); err != nil {
		return fmt.Errorf("invalid run input error: %w", invalidRunInputError())
	}

	if lease.CollectionJobKind != string(job.ID().Kind) {
		return fmt.Errorf("invalid run input error: %w", invalidRunInputError())
	}

	if maxPages < 1 || maxSuccessResponseBytes < 1 {
		return fmt.Errorf("invalid run input error: %w", invalidRunInputError())
	}

	return nil
}

func subjectMismatch(job JobContract, subject string, targets *TargetSnapshot) bool {
	if job.Class() == JobClassGlobal && subject != job.LeaseSubject() {
		return true
	}

	return job.Membership() == JobMembershipExactSubject && subject != targets.exactSubject
}

func invalidRunInputError() error {
	return collecterr.New(contract.ErrorInternalInvariant, contract.ClassInternal, "collection run input is invalid")
}

func (i *RunInput) Subject() string {
	if i == nil {
		return ""
	}

	return i.subject
}

func (i *RunInput) Lease() contract.LeaseProof {
	if i == nil {
		return contract.LeaseProof{}
	}

	return i.lease
}

func (i *RunInput) Job() JobContract {
	if i == nil {
		return JobContract{}
	}

	return i.job
}

func (i *RunInput) Generation(kind contract.ObservationKind) (int64, error) {
	if i == nil {
		return 0, collecterr.New(contract.ErrorInternalInvariant, contract.ClassInternal, "collection run input is nil")
	}

	out, err := i.contracts.Generation(kind)
	if err != nil {
		return out, fmt.Errorf("generation: %w", err)
	}

	return out, nil
}

func (i *RunInput) RequireLiveSnapshotMetadataGeneration() error {
	generation, err := i.Generation(contract.KindLiveSnapshot)
	if err != nil {
		return fmt.Errorf("live snapshot generation: %w", err)
	}

	var expected int64

	switch i.job.ID().Provider {
	case contract.ProviderYouTubeJS:
		expected = contract.LiveSnapshotQueryContractGeneration
	case contract.ProviderHolodex:
		expected = contract.LiveSnapshotMetadataContractGeneration
	case contract.ProviderHololiveOfficial:
		return collecterr.New(contract.ErrorConfiguration, contract.ClassConfiguration, "live snapshot provider is unsupported")
	default:
		return collecterr.New(contract.ErrorConfiguration, contract.ClassConfiguration, "live snapshot provider is unsupported")
	}

	if generation != expected {
		return collecterr.New(contract.ErrorConfiguration, contract.ClassConfiguration, fmt.Sprintf(
			"live snapshot contract generation %d is unsupported; collector emits generation %d",
			generation, expected,
		))
	}

	return nil
}

func (i *RunInput) Allows(kind contract.ObservationKind, subject string) (bool, error) {
	if i == nil || !kind.Valid() || subject == "" {
		return false, collecterr.New(contract.ErrorInternalInvariant, contract.ClassInternal, "collection target lookup is invalid")
	}

	if !i.job.Emits(kind) {
		return false, collecterr.New(contract.ErrorInternalInvariant, contract.ClassInternal, "runner requested non-emission kind")
	}

	out, err := i.targets.Allows(kind, subject)
	if err != nil {
		return out, fmt.Errorf("allows: %w", err)
	}

	return out, nil
}

func (i *RunInput) Roster(kind contract.ObservationKind) ([]string, error) {
	if i == nil || !kind.Valid() {
		return nil, collecterr.New(contract.ErrorInternalInvariant, contract.ClassInternal, "collection roster lookup is invalid")
	}

	if !i.job.UsesRoster(kind) {
		return nil, collecterr.New(contract.ErrorInternalInvariant, contract.ClassInternal, "runner requested non-roster kind")
	}

	out, err := i.targets.Roster(kind)
	if err != nil {
		return out, fmt.Errorf("roster: %w", err)
	}

	return out, nil
}

func (i *RunInput) MaxPages() int {
	if i == nil {
		return 0
	}

	return i.maxPages
}

func (i *RunInput) MaxSuccessResponseBytes() int {
	if i == nil {
		return 0
	}

	return i.maxSuccessResponseBytes
}

func NewRunOutput(observations []contract.Envelope, latency time.Duration) (RunOutput, error) {
	if len(observations) > MaxPublishBatchSize {
		return RunOutput{}, collecterr.New(contract.ErrorResponseTooLarge, contract.ClassResourceLimit, "observation batch exceeds publish limit")
	}

	if latency < 0 || latency > MaxCollectionLatency {
		return RunOutput{}, collecterr.New(contract.ErrorInternalInvariant, contract.ClassInternal, "collection output is invalid")
	}

	return RunOutput{
		observations:      cloneEnvelopes(observations),
		collectionLatency: latency,
	}, nil
}

func NewRunOutputWithCursors(observations []contract.Envelope, cursors []jsontext.Value, latency time.Duration) (RunOutput, error) {
	if len(cursors) != len(observations) {
		return RunOutput{}, collecterr.New(contract.ErrorInternalInvariant, contract.ClassInternal, "collection output cursor count is invalid")
	}

	out, err := NewRunOutput(observations, latency)
	if err != nil {
		return out, err
	}

	out.cursors = make([]jsontext.Value, len(cursors))
	for i := range cursors {
		out.cursors[i] = slices.Clone(cursors[i])
	}

	return out, nil
}

func OutputFromEnvelopes(envelopes []contract.Envelope, started time.Time) (RunOutput, error) {
	out, err := NewRunOutput(envelopes, ClampLatency(started))
	if err != nil {
		return out, fmt.Errorf("run output: %w", err)
	}

	return out, nil
}

func CompleteFromEnvelopes(envelopes []contract.Envelope, started time.Time) (CollectResult, error) {
	output, err := OutputFromEnvelopes(envelopes, started)
	if err != nil {
		return CollectResult{}, fmt.Errorf("output from envelopes: %w", err)
	}

	return NewCompleteResult(output), nil
}

func (o RunOutput) Observations() []contract.Envelope {
	return cloneEnvelopes(o.observations)
}

func (o RunOutput) CollectionLatency() time.Duration {
	return o.collectionLatency
}

func (o RunOutput) Cursor(index int) jsontext.Value {
	if index < 0 || index >= len(o.cursors) {
		return nil
	}

	return slices.Clone(o.cursors[index])
}

func (o RunOutput) Empty() bool {
	return len(o.observations) == 0
}

func cloneEnvelopes(values []contract.Envelope) []contract.Envelope {
	cloned := make([]contract.Envelope, len(values))
	copy(cloned, values)

	for i := range cloned {
		cloned[i].Payload = slices.Clone(cloned[i].Payload)
		if cloned[i].SourceEventAt != nil {
			value := *cloned[i].SourceEventAt

			cloned[i].SourceEventAt = &value
		}
	}

	return cloned
}
