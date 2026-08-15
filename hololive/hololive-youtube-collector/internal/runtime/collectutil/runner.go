package collectutil

import (
	"context"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/service/youtube/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/joblease"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/youtubejs"
)

type JobRunner interface {
	Provider() contract.Provider
	JobKind() string
	Emissions() []contract.ObservationKind
	TargetKinds() []contract.ObservationKind
	Collect(ctx context.Context, input *RunInput) (RunOutput, error)
}

type RunInput struct {
	Spec                joblease.JobSpec
	Lease               contract.LeaseProof
	ContractGenerations map[contract.ObservationKind]int64
	MaxPages            int
	MaxAggregateBytes   int
	EnabledSubjects     map[contract.ObservationKind][]string
}

type RunOutput struct {
	Observations      []contract.Envelope
	Checkpoints       []sourceobservation.CheckpointEntry
	CollectionLatency time.Duration
}

func ValidateInput(input *RunInput) error {
	if input == nil {
		return collecterr.New(collecterr.Failed, "collection run input is nil")
	}
	return nil
}

func Output(envelopes []contract.Envelope, started time.Time) (RunOutput, error) {
	if len(envelopes) > sourceobservation.MaxPublishBatchSize {
		return RunOutput{}, collecterr.New(collecterr.Failed, "observation batch exceeds publish limit")
	}
	checkpoints := make([]sourceobservation.CheckpointEntry, len(envelopes))
	for i := range envelopes {
		checkpoints[i] = Checkpoint(&envelopes[i])
	}
	return RunOutput{
		Observations:      envelopes,
		Checkpoints:       checkpoints,
		CollectionLatency: ClampLatency(started),
	}, nil
}

func Generation(input *RunInput, kind contract.ObservationKind) (int64, error) {
	if err := ValidateInput(input); err != nil {
		return 0, err
	}
	generation := input.ContractGenerations[kind]
	if generation <= 0 {
		return 0, collecterr.New(collecterr.Failed, "observation contract generation is missing")
	}
	return generation, nil
}

func Completeness(pageCount int, exhausted bool, continuity string) (contract.Completeness, contract.Continuity, error) {
	if pageCount < 1 {
		return "", "", collecterr.New(collecterr.ParserDrift, "page count is below 1")
	}
	return completenessForContinuity(exhausted, continuity)
}

func completenessForContinuity(exhausted bool, continuity string) (contract.Completeness, contract.Continuity, error) {
	if continuity == string(contract.ContinuityContiguous) {
		return contiguousCompleteness(exhausted)
	}
	if continuity == string(contract.ContinuityGapUnresolved) {
		return contract.CompletenessPartial, contract.ContinuityGapUnresolved, nil
	}
	if continuity == string(contract.ContinuityNotApplicable) {
		return notApplicableCompleteness(exhausted)
	}
	return "", "", collecterr.New(collecterr.ParserDrift, "unsupported continuity")
}

func contiguousCompleteness(exhausted bool) (contract.Completeness, contract.Continuity, error) {
	if exhausted {
		return contract.CompletenessComplete, contract.ContinuityContiguous, nil
	}
	return contract.CompletenessPartial, contract.ContinuityGapUnresolved, nil
}

func notApplicableCompleteness(exhausted bool) (contract.Completeness, contract.Continuity, error) {
	if exhausted {
		return contract.CompletenessComplete, contract.ContinuityNotApplicable, nil
	}
	return contract.CompletenessPartial, contract.ContinuityNotApplicable, nil
}

func PaginationOf(page youtubejs.Pagination) (contract.Completeness, contract.Continuity, error) {
	return Completeness(page.PageCount, page.Exhausted, page.Continuity)
}

func EnabledSet(input *RunInput, kind contract.ObservationKind) map[string]struct{} {
	if input == nil {
		return nil
	}
	subjects := input.EnabledSubjects[kind]
	if len(subjects) == 0 {
		return nil
	}
	result := make(map[string]struct{}, len(subjects))
	for _, subject := range subjects {
		result[subject] = struct{}{}
	}
	return result
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
