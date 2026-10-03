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

// RunInput은 lease가 고정한 job·subject·target 시야와 실행 한도를 runner에 전달합니다.
type RunInput struct {
	job                     JobContract
	subject                 string
	lease                   contract.LeaseProof
	contracts               ContractSnapshot
	targets                 TargetSnapshot
	maxPages                int
	maxSuccessResponseBytes int
}

// RunOutput은 생성 시 입력을 복사한 불변 수집 결과입니다.
// 값 복사는 내부 저장소를 공유하며, 가변 관측 조회는 독립된 복사본을 반환합니다.
type RunOutput struct {
	observations      []contract.Envelope
	cursors           []jsontext.Value
	collectionLatency time.Duration
}

// NewRunInput은 lease 증명, job 계약, lease subject와 target 시야가 같은 job·세대를 가리킬 때만 입력을 만듭니다.
// Lease 증명과 job key의 결합은 target snapshot 적재가 먼저 확인합니다.
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
		return RunInput{}, collecterr.New(collecterr.Internal, collecterr.ClassInternal, "collection target snapshot identity does not match")
	}

	if subjectMismatch(job, subject, &targets) {
		return RunInput{}, collecterr.New(collecterr.Internal, collecterr.ClassInternal, "collection run subject does not match")
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

// subjectMismatch는 GLOBAL job의 고정 lease subject와 EXACT_SUBJECT 시야의 subject를 실행 subject와 대조합니다.
func subjectMismatch(job JobContract, subject string, targets *TargetSnapshot) bool {
	if job.Class() == JobClassGlobal && subject != job.LeaseSubject() {
		return true
	}

	return job.Membership() == JobMembershipExactSubject && subject != targets.exactSubject
}

func invalidRunInputError() error {
	return collecterr.New(collecterr.Internal, collecterr.ClassInternal, "collection run input is invalid")
}

// Subject는 lease가 고정한 job subject입니다. GLOBAL job은 계약의 lease subject입니다.
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
		return 0, collecterr.New(collecterr.Internal, collecterr.ClassInternal, "collection run input is nil")
	}

	out, err := i.contracts.Generation(kind)
	if err != nil {
		return out, fmt.Errorf("generation: %w", err)
	}

	return out, nil
}

// RequireLiveSnapshotMetadataGeneration은 provider별 발행 세대를 검증합니다.
// YouTube.js의 streams 조회 증명은 generation 3이며 Holodex metadata는 generation 2를 유지합니다.
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
		// 공식 일정 provider는 방송 snapshot을 발행하지 않습니다.
		return collecterr.New(collecterr.Configuration, collecterr.ClassConfiguration, "live snapshot provider is unsupported")
	default:
		return collecterr.New(collecterr.Configuration, collecterr.ClassConfiguration, "live snapshot provider is unsupported")
	}

	if generation != expected {
		return collecterr.New(collecterr.Configuration, collecterr.ClassConfiguration, fmt.Sprintf(
			"live snapshot contract generation %d is unsupported; collector emits generation %d",
			generation, expected,
		))
	}

	return nil
}

func (i *RunInput) Allows(kind contract.ObservationKind, subject string) (bool, error) {
	if i == nil || !kind.Valid() || subject == "" {
		return false, collecterr.New(collecterr.Internal, collecterr.ClassInternal, "collection target lookup is invalid")
	}

	if !i.job.Emits(kind) {
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
		return nil, collecterr.New(collecterr.Internal, collecterr.ClassInternal, "collection roster lookup is invalid")
	}

	if !i.job.UsesRoster(kind) {
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

// NewRunOutput은 payload와 source event 시각을 포함한 입력의 소유권을 복사로 확보합니다.
func NewRunOutput(observations []contract.Envelope, latency time.Duration) (RunOutput, error) {
	if len(observations) > MaxPublishBatchSize {
		return RunOutput{}, collecterr.New(collecterr.ResponseTooLarge, collecterr.ClassResourceLimit, "observation batch exceeds publish limit")
	}

	if latency < 0 || latency > MaxCollectionLatency {
		return RunOutput{}, collecterr.New(collecterr.Internal, collecterr.ClassInternal, "collection output is invalid")
	}

	return RunOutput{
		observations:      cloneEnvelopes(observations),
		collectionLatency: latency,
	}, nil
}

// NewRunOutputWithCursors는 관측과 같은 순서의 checkpoint cursor까지 복사로 소유합니다.
// 빈 cursor는 해당 관측의 checkpoint가 cursor 없이 관측 식별자만 기록한다는 뜻입니다.
func NewRunOutputWithCursors(observations []contract.Envelope, cursors []jsontext.Value, latency time.Duration) (RunOutput, error) {
	if len(cursors) != len(observations) {
		return RunOutput{}, collecterr.New(collecterr.Internal, collecterr.ClassInternal, "collection output cursor count is invalid")
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

// Observations는 payload와 source event 시각까지 복사한 독립 관측 목록을 반환합니다.
// 발행 경로는 이 복사본 하나로 저장소 입력과 체크포인트를 만듭니다.
func (o RunOutput) Observations() []contract.Envelope {
	return cloneEnvelopes(o.observations)
}

func (o RunOutput) CollectionLatency() time.Duration {
	return o.collectionLatency
}

// Cursor는 [0, ObservationCount()) 범위 관측의 checkpoint cursor 복사본을 반환합니다. cursor가 없으면 nil입니다.
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
