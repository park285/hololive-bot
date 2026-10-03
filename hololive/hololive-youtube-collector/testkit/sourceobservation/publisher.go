// Package sourceobservation은 peer 모듈의 DB 시험에서 실제 collector publisher를 연결합니다.
package sourceobservation

import (
	"context"
	"encoding/json/jsontext"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	store "github.com/kapu/hololive-youtube-collector/internal/runtime/sourceobservation"
)

type CheckpointEntry struct {
	Provider           contract.Provider
	ObservationKind    contract.ObservationKind
	SubjectKey         string
	ScopeSHA256        string
	ContractGeneration int64
	LastObservationKey string
	LastEvidenceSHA256 string
	LastScheduledFor   time.Time
	Continuity         contract.Continuity
	Cursor             jsontext.Value
}

type CheckpointUpdate struct {
	Entries           []CheckpointEntry
	CollectionLatency time.Duration
}

type PublishBatchInput struct {
	Lease        contract.LeaseProof
	Checkpoint   CheckpointUpdate
	Observations []contract.Envelope
}

type PublishOutcome string

const (
	PublishInserted  PublishOutcome = "INSERTED"
	PublishDuplicate PublishOutcome = "DUPLICATE"
	PublishCollision PublishOutcome = "COLLISION"
)

type PublishedObservation struct {
	ObservationID int64
	Outcome       PublishOutcome
	Ordinal       int
}

func NewPublishedObservation(observationID int64, outcome PublishOutcome, ordinal int) PublishedObservation {
	return PublishedObservation{ObservationID: observationID, Outcome: outcome, Ordinal: ordinal}
}

type PublishBatchResult struct {
	Results []PublishedObservation
}

type Publisher struct {
	repository *store.Repository
}

func NewPublisher(pool *pgxpool.Pool) *Publisher {
	return &Publisher{repository: store.NewRepository(pool)}
}

// NewHistoricalViewerPublisher는 보존 중인 과거 viewer 계약의 관측을 실제 publisher로 시드합니다.
func NewHistoricalViewerPublisher(pool *pgxpool.Pool) (*Publisher, error) {
	jobs := store.InitialJobContracts()
	fixtures := []struct {
		provider          contract.Provider
		kind              store.JobKind
		class             store.JobClass
		membership        store.JobMembership
		subject           string
		emissions, roster []contract.ObservationKind
	}{
		{contract.ProviderYouTubeJS, "youtubejs_viewer", store.JobClassSubject, store.JobMembershipExactSubject, "", []contract.ObservationKind{contract.KindViewerSample}, nil},
		{contract.ProviderHolodex, "holodex_live", store.JobClassGlobal, store.JobMembershipCurrentProjection, "global:holodex_live", []contract.ObservationKind{contract.KindLiveSnapshot, contract.KindViewerSample}, []contract.ObservationKind{contract.KindLiveSnapshot}},
	}

	for i := range fixtures {
		fixture := &fixtures[i]
		id := store.JobID{Provider: fixture.provider, Kind: fixture.kind}

		job, err := store.NewJobContract(id, fixture.class, fixture.membership, fixture.subject, fixture.emissions, fixture.emissions, fixture.roster)
		if err != nil {
			return nil, fmt.Errorf("create historical viewer job: %w", err)
		}

		jobs[id] = job
	}

	return &Publisher{repository: store.NewRepositoryWithContracts(pool, jobs, nil)}, nil
}

func (p *Publisher) PublishBatch(ctx context.Context, input *PublishBatchInput) (PublishBatchResult, error) {
	var converted *store.PublishBatchInput

	if input != nil {
		entries := make([]store.CheckpointEntry, len(input.Checkpoint.Entries))
		for i := range input.Checkpoint.Entries {
			entries[i] = store.CheckpointEntry(input.Checkpoint.Entries[i])
		}

		converted = &store.PublishBatchInput{Lease: input.Lease, Observations: input.Observations, Checkpoint: store.CheckpointUpdate{Entries: entries, CollectionLatency: input.Checkpoint.CollectionLatency}}
	}

	result, err := p.repository.PublishBatch(ctx, converted)
	out := PublishBatchResult{Results: make([]PublishedObservation, len(result.Results))}

	for i, row := range result.Results {
		out.Results[i] = PublishedObservation{ObservationID: row.ObservationID, Outcome: PublishOutcome(row.Outcome), Ordinal: row.Ordinal}
	}

	if err != nil {
		return out, fmt.Errorf("publish fixture batch: %w", err)
	}

	return out, nil
}
