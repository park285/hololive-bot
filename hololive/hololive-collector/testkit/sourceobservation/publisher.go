package sourceobservation

import (
	"context"
	"encoding/json/jsontext"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kapu/hololive-collector/internal/runtime/collection"
	store "github.com/kapu/hololive-collector/internal/runtime/sourceobservation"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

var ErrTargetDisabled = collection.ErrTargetDisabled

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

type PublishBatchResult struct {
	Results []PublishedObservation
}

type Publisher struct {
	repository *store.Repository
}

func NewPublisher(pool *pgxpool.Pool) *Publisher {
	return &Publisher{repository: store.NewRepository(pool)}
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
