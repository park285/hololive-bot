package sourceobservation

import (
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"strings"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

const (
	MaxPublishBatchBytes = 8 << 20
	MaxCheckpointCount   = 1024
)

var (
	ErrInvalidEnvelope   = errors.New("source observation envelope is invalid")
	ErrStaleContract     = errors.New("source observation contract is stale")
	ErrInvalidRepository = errors.New("source observation repository is not configured")
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
	ObservationID       int64
	Outcome             PublishOutcome
	Ordinal             int
	AcceptedInterval    time.Duration
	HasAcceptedInterval bool
}

type PublishBatchResult struct {
	Results      []PublishedObservation
	EncodedBytes int
}

func validateText(name, value string, maxLength int) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s is empty", name)
	}

	if strings.TrimSpace(value) != value {
		return fmt.Errorf("%s must not contain surrounding whitespace", name)
	}

	if len(value) > maxLength {
		return fmt.Errorf("%s exceeds %d bytes", name, maxLength)
	}

	return nil
}
