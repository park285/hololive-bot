package collectutil

import (
	"context"
	"fmt"
	"slices"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/service/youtube/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/joblease"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/youtubejs"
)

type JobRunner interface {
	JobID() sourceobservation.JobID
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
		//nolint:wrapcheck // 오류 생성자가 만든 값이라 감쌀 하위 오류가 없다.
		return 0, collecterr.New(collecterr.Internal, collecterr.ClassInternal, "observation contract generation is missing")
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

func (s ContractSnapshot) Kinds() []contract.ObservationKind {
	kinds := make([]contract.ObservationKind, 0, len(s.generations))
	for kind := range s.generations {
		kinds = append(kinds, kind)
	}

	slices.Sort(kinds)

	return kinds
}

type RunInput struct {
	spec                    joblease.JobSpec
	lease                   contract.LeaseProof
	job                     sourceobservation.JobContract
	contracts               ContractSnapshot
	targets                 joblease.TargetSnapshot
	maxPages                int
	maxSuccessResponseBytes int
}

type RunOutput struct {
	observations      []contract.Envelope
	checkpoints       []sourceobservation.CheckpointEntry
	collectionLatency time.Duration
}

func NewRunInput(
	spec *joblease.JobSpec,
	lease *contract.LeaseProof,
	contracts ContractSnapshot,
	targets joblease.TargetSnapshot,
	maxPages, maxSuccessResponseBytes int,
	job sourceobservation.JobContract,
) (RunInput, error) {
	if spec == nil || lease == nil {
		//nolint:wrapcheck // 오류 생성자가 만든 값이라 감쌀 하위 오류가 없다.
		return RunInput{}, collecterr.New(collecterr.Internal, collecterr.ClassInternal, "collection run input is invalid")
	}

	if err := validateRunInputIdentity(spec, lease, job, maxPages, maxSuccessResponseBytes); err != nil {
		return RunInput{}, fmt.Errorf("validate run input identity: %w", err)
	}

	if err := contracts.ValidateKinds(job.Emissions()); err != nil {
		return RunInput{}, fmt.Errorf("validate kinds: %w", err)
	}

	if targets.Generation() != lease.ProjectionGeneration || targets.Membership() != job.Membership() {
		//nolint:wrapcheck // 오류 생성자가 만든 값이라 감쌀 하위 오류가 없다.
		return RunInput{}, collecterr.New(collecterr.Internal, collecterr.ClassInternal, "collection target snapshot identity does not match")
	}

	if err := targets.ValidateRequested(job.RequestedKinds()); err != nil {
		return RunInput{}, fmt.Errorf("validate requested: %w", err)
	}

	return RunInput{
		spec: *spec, lease: *lease, job: job.Clone(), contracts: contracts,
		targets: targets.Clone(), maxPages: maxPages, maxSuccessResponseBytes: maxSuccessResponseBytes,
	}, nil
}

func validateRunInputIdentity(
	spec *joblease.JobSpec,
	lease *contract.LeaseProof,
	job sourceobservation.JobContract,
	maxPages, maxSuccessResponseBytes int,
) error {
	if err := job.Validate(); err != nil {
		return fmt.Errorf("invalid run input error: %w", invalidRunInputError())
	}

	if job.ID().Provider != spec.Provider || string(job.ID().Kind) != spec.CollectionJobKind {
		return fmt.Errorf("invalid run input error: %w", invalidRunInputError())
	}

	if lease.JobKey != spec.JobKey || lease.CollectionJobKind != spec.CollectionJobKind {
		return fmt.Errorf("invalid run input error: %w", invalidRunInputError())
	}

	if maxPages < 1 || maxSuccessResponseBytes < 1 {
		return fmt.Errorf("invalid run input error: %w", invalidRunInputError())
	}

	return nil
}

func invalidRunInputError() error {
	//nolint:wrapcheck // 오류 생성자가 만든 값이라 감쌀 하위 오류가 없다.
	return collecterr.New(collecterr.Internal, collecterr.ClassInternal, "collection run input is invalid")
}

func (i *RunInput) Spec() joblease.JobSpec {
	if i == nil {
		return joblease.JobSpec{}
	}

	return i.spec
}

func (i *RunInput) Lease() contract.LeaseProof {
	if i == nil {
		return contract.LeaseProof{}
	}

	return i.lease
}

func (i *RunInput) Job() sourceobservation.JobContract {
	if i == nil {
		return sourceobservation.JobContract{}
	}

	return i.job.Clone()
}

func (i *RunInput) Generation(kind contract.ObservationKind) (int64, error) {
	if i == nil {
		//nolint:wrapcheck // 오류 생성자가 만든 값이라 감쌀 하위 오류가 없다.
		return 0, collecterr.New(collecterr.Internal, collecterr.ClassInternal, "collection run input is nil")
	}

	out, err := i.contracts.Generation(kind)
	if err != nil {
		return out, fmt.Errorf("generation: %w", err)
	}

	return out, nil
}

func (i *RunInput) Allows(kind contract.ObservationKind, subject string) (bool, error) {
	if i == nil || !kind.Valid() || subject == "" {
		//nolint:wrapcheck // 오류 생성자가 만든 값이라 감쌀 하위 오류가 없다.
		return false, collecterr.New(collecterr.Internal, collecterr.ClassInternal, "collection target lookup is invalid")
	}

	if !i.job.Emits(kind) {
		//nolint:wrapcheck // 오류 생성자가 만든 값이라 감쌀 하위 오류가 없다.
		return false, collecterr.New(collecterr.Internal, collecterr.ClassInternal, "runner requested non-emission kind")
	}

	out, err := i.targets.Allows(kind, subject)
	if err != nil {
		return out, fmt.Errorf("allows: %w", err)
	}

	return out, nil
}

func (i *RunInput) Roster(kind contract.ObservationKind) ([]string, error) {
	if i == nil || !kind.Valid() {
		//nolint:wrapcheck // 오류 생성자가 만든 값이라 감쌀 하위 오류가 없다.
		return nil, collecterr.New(collecterr.Internal, collecterr.ClassInternal, "collection roster lookup is invalid")
	}

	if !i.job.UsesRoster(kind) {
		//nolint:wrapcheck // 오류 생성자가 만든 값이라 감쌀 하위 오류가 없다.
		return nil, collecterr.New(collecterr.Internal, collecterr.ClassInternal, "runner requested non-roster kind")
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

func NewRunOutput(
	observations []contract.Envelope,
	checkpoints []sourceobservation.CheckpointEntry,
	latency time.Duration,
) (RunOutput, error) {
	if len(observations) > sourceobservation.MaxPublishBatchSize {
		//nolint:wrapcheck // 오류 생성자가 만든 값이라 감쌀 하위 오류가 없다.
		return RunOutput{}, collecterr.New(collecterr.ResponseTooLarge, collecterr.ClassResourceLimit, "observation batch exceeds publish limit")
	}

	if len(observations) != len(checkpoints) || latency < 0 || latency > sourceobservation.MaxCollectionLatency {
		//nolint:wrapcheck // 오류 생성자가 만든 값이라 감쌀 하위 오류가 없다.
		return RunOutput{}, collecterr.New(collecterr.Internal, collecterr.ClassInternal, "collection output is invalid")
	}

	return RunOutput{
		observations:      cloneEnvelopes(observations),
		checkpoints:       cloneCheckpoints(checkpoints),
		collectionLatency: latency,
	}, nil
}

func OutputFromEnvelopes(envelopes []contract.Envelope, started time.Time) (RunOutput, error) {
	checkpoints := make([]sourceobservation.CheckpointEntry, len(envelopes))
	for i := range envelopes {
		checkpoints[i] = Checkpoint(&envelopes[i])
	}

	out, err := NewRunOutput(envelopes, checkpoints, ClampLatency(started))
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

	out, err := NewCompleteResult(output)
	if err != nil {
		return out, fmt.Errorf("complete result: %w", err)
	}

	return out, nil
}

func (o RunOutput) Observations() []contract.Envelope {
	return cloneEnvelopes(o.observations)
}

func (o RunOutput) Checkpoints() []sourceobservation.CheckpointEntry {
	return cloneCheckpoints(o.checkpoints)
}

func (o RunOutput) CollectionLatency() time.Duration {
	return o.collectionLatency
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

func cloneCheckpoints(values []sourceobservation.CheckpointEntry) []sourceobservation.CheckpointEntry {
	cloned := make([]sourceobservation.CheckpointEntry, len(values))
	copy(cloned, values)

	for i := range cloned {
		cloned[i].Cursor = slices.Clone(cloned[i].Cursor)
	}

	return cloned
}

func PaginationOf(page *youtubejs.Pagination) (contract.Completeness, contract.Continuity, error) {
	if page == nil {
		//nolint:wrapcheck // 오류 생성자가 만든 값이라 감쌀 하위 오류가 없다.
		return "", "", collecterr.New(collecterr.Internal, collecterr.ClassInternal, "pagination is nil")
	}

	completeness, continuity, err := page.Quality()
	if err != nil {
		return "", "", collecterr.Wrap(collecterr.HelperProtocolMismatch, collecterr.ClassProtocol, err)
	}

	return completeness, continuity, nil
}

func SampleWindowSeconds(interval time.Duration) int {
	seconds := int(interval / time.Second)
	if seconds < 1 {
		return 60
	}

	if seconds > 86400 {
		return 86400
	}

	return seconds
}

func DefaultMaxResults() int {
	return 10
}

func MaxResults(value int) int {
	if value <= 0 {
		return DefaultMaxResults()
	}

	return value
}
