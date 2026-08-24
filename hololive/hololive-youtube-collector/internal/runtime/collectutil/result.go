package collectutil

import (
	"fmt"
	"slices"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
)

type CollectResultKind string

const (
	CollectComplete CollectResultKind = "COMPLETE"
	CollectPartial  CollectResultKind = "PARTIAL"
)

type PartialFailure struct {
	failedKinds []contract.ObservationKind
	cause       error
}

type CollectResult struct {
	kind    CollectResultKind
	output  RunOutput
	partial *PartialFailure
}

func NewCompleteResult(output RunOutput) (CollectResult, error) {
	if len(output.observations) != len(output.checkpoints) {
		//nolint:wrapcheck // 오류 생성자가 만든 값이라 감쌀 하위 오류가 없다.
		return CollectResult{}, collecterr.New(collecterr.Internal, collecterr.ClassInternal, "complete result output is invalid")
	}

	return CollectResult{kind: CollectComplete, output: cloneRunOutput(output)}, nil
}

func NewPartialResult(output RunOutput, cause error, failedKinds ...contract.ObservationKind) (CollectResult, error) {
	if output.Empty() || len(failedKinds) == 0 || cause == nil {
		//nolint:wrapcheck // 오류 생성자가 만든 값이라 감쌀 하위 오류가 없다.
		return CollectResult{}, collecterr.New(collecterr.Internal, collecterr.ClassInternal, "partial result is incomplete")
	}

	normalized, err := normalizePartialCause(cause)
	if err != nil {
		return CollectResult{}, fmt.Errorf("normalize partial cause: %w", err)
	}

	failed := slices.Clone(failedKinds)
	slices.Sort(failed)

	failed = slices.Compact(failed)
	if err := validatePartialKinds(output.observations, failed); err != nil {
		return CollectResult{}, fmt.Errorf("validate partial kinds: %w", err)
	}

	return CollectResult{
		kind: CollectPartial, output: cloneRunOutput(output),
		partial: &PartialFailure{failedKinds: failed, cause: normalized},
	}, nil
}

func normalizePartialCause(cause error) (normalized *collecterr.Error, validationErr error) {
	if cause == nil {
		//nolint:wrapcheck // 오류 생성자가 만든 값이라 감쌀 하위 오류가 없다.
		return nil, collecterr.New(collecterr.Internal, collecterr.ClassInternal, "partial result failure class is not allowed")
	}

	normalized = collecterr.Normalize(cause)
	if !PartialFailureClassAllowed(collecterr.ClassOf(normalized)) {
		//nolint:wrapcheck // 오류 생성자가 만든 값이라 감쌀 하위 오류가 없다.
		return nil, collecterr.New(collecterr.Internal, collecterr.ClassInternal, "partial result failure class is not allowed")
	}

	return normalized, nil
}

func validatePartialKinds(observations []contract.Envelope, failed []contract.ObservationKind) error {
	emitted := make(map[contract.ObservationKind]struct{}, len(observations))
	for i := range observations {
		emitted[observations[i].ObservationKind] = struct{}{}
	}

	for _, kind := range failed {
		if !kind.Valid() {
			//nolint:wrapcheck // 오류 생성자가 만든 값이라 감쌀 하위 오류가 없다.
			return collecterr.New(collecterr.Internal, collecterr.ClassInternal, "partial result contains invalid failed kind")
		}

		if _, ok := emitted[kind]; ok {
			//nolint:wrapcheck // 오류 생성자가 만든 값이라 감쌀 하위 오류가 없다.
			return collecterr.New(collecterr.Internal, collecterr.ClassInternal, "partial result failed and emitted kinds overlap")
		}
	}

	return nil
}

func PartialFailureClassAllowed(class contract.FailureClass) bool {
	switch class {
	case collecterr.ClassTransient, collecterr.ClassTimeout, collecterr.ClassCooldown,
		collecterr.ClassDataContract, collecterr.ClassResourceLimit, collecterr.ClassConfiguration:
		return true
	case collecterr.ClassCanceled, collecterr.ClassProtocol, collecterr.ClassSuperseded, collecterr.ClassInternal:
		return false
	default:
		return false
	}
}

func (r *CollectResult) Kind() CollectResultKind {
	if r == nil {
		return ""
	}

	return r.kind
}

func (r *CollectResult) Output() RunOutput {
	if r == nil {
		return RunOutput{}
	}

	return cloneRunOutput(r.output)
}

func (r *CollectResult) PartialFailure() (*PartialFailure, bool) {
	if r == nil {
		return nil, false
	}

	if r.partial == nil {
		return nil, false
	}

	return &PartialFailure{failedKinds: slices.Clone(r.partial.failedKinds), cause: r.partial.cause}, true
}

func (r *CollectResult) IsZero() bool {
	if r == nil {
		return true
	}

	return r.kind == "" && r.partial == nil && r.output.Empty() && r.output.collectionLatency == 0
}

func (p *PartialFailure) Cause() error {
	if p == nil {
		return nil
	}

	return p.cause
}

func (p *PartialFailure) FailedKinds() []contract.ObservationKind {
	if p == nil {
		return nil
	}

	return slices.Clone(p.failedKinds)
}

func cloneRunOutput(output RunOutput) RunOutput {
	return RunOutput{
		observations:      cloneEnvelopes(output.observations),
		checkpoints:       cloneCheckpoints(output.checkpoints),
		collectionLatency: output.collectionLatency,
	}
}
